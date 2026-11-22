package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (c *client) apiGet(ctx context.Context, pathAndQuery string) (*http.Response, []byte, error) {
	tok, err := c.accessToken(ctx)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+pathAndQuery, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/vnd.api+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("GET %s: %w", pathAndQuery, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("read body: %w", err)
	}
	return resp, body, nil
}

// printMeta prints status plus any rate limit related headers.
func printMeta(resp *http.Response) {
	fmt.Println("status:", resp.Status)
	for k, v := range resp.Header {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "ratelimit") || strings.Contains(lk, "rate-limit") || lk == "retry-after" {
			fmt.Printf("header %s: %s\n", k, strings.Join(v, ", "))
		}
	}
}

func (c *client) getCmd(ctx context.Context, pathAndQuery string) error {
	resp, body, err := c.apiGet(ctx, pathAndQuery)
	if err != nil {
		return err
	}
	printMeta(resp)
	fmt.Println(truncate(prettyJSON(body), 6000))
	return nil
}

func prettyJSON(b []byte) string {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return string(b)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(b)
	}
	return string(out)
}

type manifestOpts struct {
	trackID      string
	manifestType string
	formats      string
	uriScheme    string
	adaptive     bool
}

func parseManifestFlags(name string, args []string) (manifestOpts, *flag.FlagSet, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	var o manifestOpts
	fs.StringVar(&o.manifestType, "type", "MPEG_DASH", "manifestType: MPEG_DASH or HLS")
	fs.StringVar(&o.formats, "formats", "FLAC_HIRES,FLAC", "comma-separated formats")
	fs.StringVar(&o.uriScheme, "uri", "HTTPS", "uriScheme: HTTPS or DATA")
	fs.BoolVar(&o.adaptive, "adaptive", false, "adaptive flag")
	if err := fs.Parse(args); err != nil {
		return o, fs, err
	}
	if fs.NArg() < 1 {
		return o, fs, fmt.Errorf("usage: %s [flags] <track-id>", name)
	}
	o.trackID = fs.Arg(0)
	return o, fs, nil
}

type manifestDoc struct {
	Data struct {
		ID         string `json:"id"`
		Attributes struct {
			URI               string   `json:"uri"`
			Formats           []string `json:"formats"`
			Hash              string   `json:"hash"`
			TrackPresentation string   `json:"trackPresentation"`
			PreviewReason     string   `json:"previewReason"`
			DrmData           *struct {
				DrmSystem      string   `json:"drmSystem"`
				LicenseURL     string   `json:"licenseUrl"`
				CertificateURL string   `json:"certificateUrl"`
				InitData       []string `json:"initData"`
			} `json:"drmData"`
			TrackAudioNormalizationData *struct {
				ReplayGain    float64 `json:"replayGain"`
				PeakAmplitude float64 `json:"peakAmplitude"`
			} `json:"trackAudioNormalizationData"`
		} `json:"attributes"`
	} `json:"data"`
}

func (c *client) fetchManifest(ctx context.Context, o manifestOpts) (manifestDoc, error) {
	q := url.Values{}
	q.Set("manifestType", o.manifestType)
	for _, f := range strings.Split(o.formats, ",") {
		q.Add("formats", strings.TrimSpace(f))
	}
	q.Set("uriScheme", o.uriScheme)
	q.Set("usage", "PLAYBACK")
	q.Set("adaptive", fmt.Sprint(o.adaptive))
	resp, body, err := c.apiGet(ctx, "/trackManifests/"+url.PathEscape(o.trackID)+"?"+q.Encode())
	if err != nil {
		return manifestDoc{}, err
	}
	printMeta(resp)
	if resp.StatusCode != http.StatusOK {
		fmt.Println(truncate(prettyJSON(body), 3000))
		return manifestDoc{}, fmt.Errorf("trackManifests returned %s", resp.Status)
	}
	var m manifestDoc
	if err := json.Unmarshal(body, &m); err != nil {
		return manifestDoc{}, fmt.Errorf("decode manifest doc: %w", err)
	}
	a := m.Data.Attributes
	fmt.Println("trackPresentation:", a.TrackPresentation)
	fmt.Println("previewReason:    ", a.PreviewReason)
	fmt.Println("formats:          ", a.Formats)
	if a.DrmData != nil {
		fmt.Printf("drmData:           PRESENT system=%s licenseUrl-set=%v initData=%d\n",
			a.DrmData.DrmSystem, a.DrmData.LicenseURL != "", len(a.DrmData.InitData))
	} else {
		fmt.Println("drmData:           absent")
	}
	if n := a.TrackAudioNormalizationData; n != nil {
		fmt.Printf("replayGain:        %.2f dB (peak %.4f)\n", n.ReplayGain, n.PeakAmplitude)
	}
	fmt.Println("uri:              ", describeURI(a.URI))
	return m, nil
}

// describeURI summarizes a manifest URI without exposing signatures.
func describeURI(u string) string {
	if strings.HasPrefix(u, "data:") {
		head, _, _ := strings.Cut(u, ",")
		return fmt.Sprintf("%s,... (%d bytes)", head, len(u))
	}
	p, err := url.Parse(u)
	if err != nil {
		return fmt.Sprintf("(unparseable, %d bytes)", len(u))
	}
	return fmt.Sprintf("%s://%s%s (query %d bytes, redacted)", p.Scheme, p.Host, p.Path, len(p.RawQuery))
}

// loadManifestBody returns the manifest document the URI points to.
func (c *client) loadManifestBody(ctx context.Context, uri string) ([]byte, error) {
	if strings.HasPrefix(uri, "data:") {
		head, payload, ok := strings.Cut(uri, ",")
		if !ok {
			return nil, errors.New("malformed data URI")
		}
		if strings.HasSuffix(head, ";base64") {
			return base64.StdEncoding.DecodeString(payload)
		}
		s, err := url.PathUnescape(payload)
		return []byte(s), err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch manifest: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// mpd is the subset of a DASH MPD needed to see codec, rate, and protection.
type mpd struct {
	MediaPresentationDuration string `xml:"mediaPresentationDuration,attr"`
	Periods                   []struct {
		AdaptationSets []struct {
			MimeType          string     `xml:"mimeType,attr"`
			Codecs            string     `xml:"codecs,attr"`
			AudioSamplingRate string     `xml:"audioSamplingRate,attr"`
			ContentProtection []xmlAnyEl `xml:"ContentProtection"`
			Representations   []mpdRepEl `xml:"Representation"`
		} `xml:"AdaptationSet"`
	} `xml:"Period"`
}

type mpdRepEl struct {
	ID                string     `xml:"id,attr"`
	Codecs            string     `xml:"codecs,attr"`
	Bandwidth         string     `xml:"bandwidth,attr"`
	AudioSamplingRate string     `xml:"audioSamplingRate,attr"`
	ContentProtection []xmlAnyEl `xml:"ContentProtection"`
}

type xmlAnyEl struct {
	SchemeIDURI string `xml:"schemeIdUri,attr"`
}

func summarizeManifest(body []byte) {
	trim := strings.TrimSpace(string(body))
	switch {
	case strings.HasPrefix(trim, "<"):
		var m mpd
		if err := xml.Unmarshal(body, &m); err != nil {
			fmt.Println("MPD parse error:", err)
			return
		}
		fmt.Println("MPD duration:", m.MediaPresentationDuration)
		for pi, p := range m.Periods {
			for ai, as := range p.AdaptationSets {
				fmt.Printf("period %d set %d: mime=%s codecs=%s rate=%s protection=%d\n",
					pi, ai, as.MimeType, as.Codecs, as.AudioSamplingRate, len(as.ContentProtection))
				for _, cp := range as.ContentProtection {
					fmt.Println("  ContentProtection:", cp.SchemeIDURI)
				}
				for _, r := range as.Representations {
					fmt.Printf("  rep %s: codecs=%s bandwidth=%s rate=%s protection=%d\n",
						r.ID, r.Codecs, r.Bandwidth, r.AudioSamplingRate, len(r.ContentProtection))
				}
			}
		}
	case strings.HasPrefix(trim, "#EXTM3U"):
		for _, line := range strings.Split(trim, "\n") {
			if strings.HasPrefix(line, "#") {
				fmt.Println(" ", line)
			}
		}
	default:
		fmt.Printf("unrecognized manifest body (%d bytes)\n", len(body))
	}
}

func (c *client) manifestCmd(ctx context.Context, args []string) error {
	o, _, err := parseManifestFlags("manifest", args)
	if err != nil {
		return err
	}
	m, err := c.fetchManifest(ctx, o)
	if err != nil {
		return err
	}
	body, err := c.loadManifestBody(ctx, m.Data.Attributes.URI)
	if err != nil {
		return err
	}
	summarizeManifest(body)
	return nil
}

// playCmd hands the manifest to mpv. Extra mpv flags follow "--".
func (c *client) playCmd(ctx context.Context, args []string) error {
	var mpvArgs []string
	for i, a := range args {
		if a == "--" {
			mpvArgs = args[i+1:]
			args = args[:i]
			break
		}
	}
	o, _, err := parseManifestFlags("play", args)
	if err != nil {
		return err
	}
	m, err := c.fetchManifest(ctx, o)
	if err != nil {
		return err
	}
	uri := m.Data.Attributes.URI
	target := uri
	if strings.HasPrefix(uri, "data:") || o.manifestType == "MPEG_DASH" {
		// Write the manifest to a private temp file so the signed URL never
		// appears in the process list.
		body, err := c.loadManifestBody(ctx, uri)
		if err != nil {
			return err
		}
		dir, err := os.MkdirTemp("", "tidal-spike-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		ext := ".mpd"
		if o.manifestType == "HLS" {
			ext = ".m3u8"
		}
		target = filepath.Join(dir, "manifest"+ext)
		if err := os.WriteFile(target, body, 0o600); err != nil {
			return err
		}
	}
	// A manifest opened from a local file only gets FFmpeg's file protocols by
	// default; the segments are absolute https URLs, so allow network ones.
	base := []string{"--no-video", "--end=20", "--msg-level=all=status,ao=v,cplayer=v",
		"--demuxer-lavf-o=protocol_whitelist=[file,http,https,tls,tcp,crypto,data]"}
	cmd := exec.CommandContext(ctx, "mpv", append(append(base, mpvArgs...), target)...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	fmt.Println("running mpv for 20s:", strings.Join(append(base, mpvArgs...), " "), "<manifest>")
	return cmd.Run()
}
