package tidal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
)

// testNow matches the clock in newTestClient.
var testNow = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

// cfPolicy builds a CloudFront custom policy query value expiring at t.
func cfPolicy(t time.Time) string {
	p := fmt.Sprintf(`{"Statement":[{"Resource":"https://sp-ad-cf.audio.tidal.com/*","Condition":{"DateLessThan":{"AWS:EpochTime":%d}}}]}`, t.Unix())
	return strings.NewReplacer("+", "-", "=", "_", "/", "~").Replace(base64.StdEncoding.EncodeToString([]byte(p)))
}

type mpdOpts struct {
	query      string // segment URL query string (unescaped &)
	mediaQuery string // media template query; defaults to query
	protection string // "", "mpd", "set", or "rep"
	duration   string
	rate       string
}

func buildMPD(o mpdOpts) string {
	if o.duration == "" {
		o.duration = "PT29.994S"
	}
	if o.rate == "" {
		o.rate = "192000"
	}
	cp := `<ContentProtection schemeIdUri="urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"/>`
	at := func(where string) string {
		if o.protection == where {
			return cp
		}
		return ""
	}
	q := strings.ReplaceAll(o.query, "&", "&amp;")
	mq := q
	if o.mediaQuery != "" {
		mq = strings.ReplaceAll(o.mediaQuery, "&", "&amp;")
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<MPD xmlns="urn:mpeg:dash:schema:mpd:2011" type="static" minBufferTime="PT3.989S" mediaPresentationDuration="` + o.duration + `">` + at("mpd") + `
 <Period id="0"><AdaptationSet id="0" contentType="audio" mimeType="audio/mp4">` + at("set") + `
  <Representation id="FLAC_HIRES,` + o.rate + `,24" codecs="flac" bandwidth="4325950" audioSamplingRate="` + o.rate + `">` + at("rep") + `
   <SegmentTemplate timescale="` + o.rate + `" initialization="https://sp-ad-cf.audio.tidal.com/mediatracks/abc/0.mp4?` + q + `" media="https://sp-ad-cf.audio.tidal.com/mediatracks/abc/$Number$.mp4?` + mq + `" startNumber="1"/>
  </Representation>
 </AdaptationSet></Period>
</MPD>`
}

func dataURI(mpd string) string {
	return "data:application/dash+xml;base64," + base64.StdEncoding.EncodeToString([]byte(mpd))
}

func manifestDoc(t *testing.T, attrs map[string]any) string {
	t.Helper()
	base := map[string]any{
		"formats":                     []string{"FLAC_HIRES"},
		"trackPresentation":           "FULL",
		"hash":                        "h1",
		"trackAudioNormalizationData": map[string]any{"replayGain": -2.07, "peakAmplitude": 0.8939},
		"albumAudioNormalizationData": map[string]any{"replayGain": -3.5, "peakAmplitude": 0.99},
	}
	for k, v := range attrs {
		if v == nil {
			delete(base, k)
			continue
		}
		base[k] = v
	}
	b, err := json.Marshal(map[string]any{"data": map[string]any{"id": "330930030", "type": "trackManifests", "attributes": base}})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const signedQuery = "Policy=%s&Signature=c2lnbmF0dXJlLXNlY3JldA__&Key-Pair-Id=K14LZCZ9QUI4JL"

func TestTrackManifestSuccess(t *testing.T) {
	expiry := testNow.Add(60 * time.Minute)
	mpd := buildMPD(mpdOpts{query: fmt.Sprintf(signedQuery, cfPolicy(expiry))})
	f := newFakeAPI(t, reply{status: 200, body: manifestDoc(t, map[string]any{"uri": dataURI(mpd)})})
	c, _ := newTestClient(t, f, &stubTokens{token: tokenA})

	m, err := c.TrackManifest(context.Background(), "330930030", []Format{FormatFLACHires, FormatFLAC, FormatAACLC})
	if err != nil {
		t.Fatal(err)
	}

	req := f.requests()[0]
	if req.path != "/v2/trackManifests/330930030" {
		t.Errorf("path = %s", req.path)
	}
	wantQuery := map[string][]string{
		"formats": {"FLAC_HIRES", "FLAC", "AACLC"}, "manifestType": {"MPEG_DASH"},
		"uriScheme": {"DATA"}, "usage": {"PLAYBACK"}, "adaptive": {"false"},
	}
	for k, v := range wantQuery {
		if !slices.Equal(req.query[k], v) {
			t.Errorf("query %s = %v, want %v", k, req.query[k], v)
		}
	}
	if !slices.Equal(m.Formats, []Format{FormatFLACHires}) || m.Presentation != PresentationFull || m.Preview() {
		t.Errorf("formats/presentation = %v %s preview=%v", m.Formats, m.Presentation, m.Preview())
	}
	if m.TrackID != "330930030" || m.Hash != "h1" || string(m.MPD) != mpd {
		t.Errorf("identity fields wrong: %s hash=%s", m.TrackID, m.Hash)
	}
	if m.SampleRate != 192000 || m.Duration != 29994*time.Millisecond {
		t.Errorf("sample rate %d duration %s", m.SampleRate, m.Duration)
	}
	if !m.FetchedAt.Equal(testNow) || !m.ExpiresAt.Equal(expiry) {
		t.Errorf("fetched %s expires %s, want %s / %s", m.FetchedAt, m.ExpiresAt, testNow, expiry)
	}
	if *m.TrackNormalization != (Normalization{-2.07, 0.8939}) || *m.AlbumNormalization != (Normalization{-3.5, 0.99}) {
		t.Errorf("normalization = %+v %+v", m.TrackNormalization, m.AlbumNormalization)
	}
	if !m.ValidFor(testNow, 59*time.Minute) || m.ValidFor(testNow, 60*time.Minute) {
		t.Error("ValidFor does not respect ExpiresAt")
	}
}

func TestTrackManifestVariants(t *testing.T) {
	policy := func(d time.Duration) string { return fmt.Sprintf(signedQuery, cfPolicy(testNow.Add(d))) }
	tests := []struct {
		name        string
		attrs       map[string]any
		mpd         mpdOpts
		wantPreview bool
		wantReason  PreviewReason
		wantExpiry  time.Time
		wantRate    int
		wantDur     time.Duration
		wantNorm    bool
	}{
		{
			name:        "preview with reason",
			attrs:       map[string]any{"trackPresentation": "PREVIEW", "previewReason": "FULL_REQUIRES_HIGHER_ACCESS_TIER"},
			mpd:         mpdOpts{query: policy(time.Hour)},
			wantPreview: true, wantReason: PreviewRequiresHigherAccessTier,
			wantExpiry: testNow.Add(time.Hour), wantRate: 192000, wantDur: 29994 * time.Millisecond, wantNorm: true,
		},
		{
			name:        "unknown presentation is treated as a preview",
			attrs:       map[string]any{"trackPresentation": "SOMETHING_NEW"},
			mpd:         mpdOpts{query: policy(time.Hour)},
			wantPreview: true, wantExpiry: testNow.Add(time.Hour), wantRate: 192000, wantDur: 29994 * time.Millisecond, wantNorm: true,
		},
		{
			name:       "canned Expires signature",
			mpd:        mpdOpts{query: fmt.Sprintf("Expires=%d&Signature=x&Key-Pair-Id=k", testNow.Add(45*time.Minute).Unix()), rate: "44100", duration: "PT1H2M3.5S"},
			wantExpiry: testNow.Add(45 * time.Minute), wantRate: 44100, wantDur: time.Hour + 2*time.Minute + 3500*time.Millisecond, wantNorm: true,
		},
		{
			name:       "earliest segment expiry wins",
			mpd:        mpdOpts{query: policy(time.Hour), mediaQuery: policy(40 * time.Minute)},
			wantExpiry: testNow.Add(40 * time.Minute), wantRate: 192000, wantDur: 29994 * time.Millisecond, wantNorm: true,
		},
		{
			name:       "earliest segment expiry wins in either order",
			mpd:        mpdOpts{query: policy(40 * time.Minute), mediaQuery: policy(time.Hour)},
			wantExpiry: testNow.Add(40 * time.Minute), wantRate: 192000, wantDur: 29994 * time.Millisecond, wantNorm: true,
		},
		{
			name:       "unsigned URLs fall back to 50 minutes",
			attrs:      map[string]any{"trackAudioNormalizationData": nil, "albumAudioNormalizationData": nil},
			mpd:        mpdOpts{query: "x=1", duration: "bogus", rate: "n/a"},
			wantExpiry: testNow.Add(50 * time.Minute),
		},
		{
			name:       "expiry behind the local clock falls back",
			mpd:        mpdOpts{query: policy(-time.Hour)},
			wantExpiry: testNow.Add(50 * time.Minute), wantRate: 192000, wantDur: 29994 * time.Millisecond, wantNorm: true,
		},
		{
			name:       "undecodable policy falls back",
			mpd:        mpdOpts{query: "Policy=%%%&Signature=x"},
			wantExpiry: testNow.Add(50 * time.Minute), wantRate: 192000, wantDur: 29994 * time.Millisecond, wantNorm: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs := map[string]any{"uri": dataURI(buildMPD(tt.mpd))}
			maps.Copy(attrs, tt.attrs)
			f := newFakeAPI(t, reply{status: 200, body: manifestDoc(t, attrs)})
			c, _ := newTestClient(t, f, &stubTokens{token: tokenA})

			m, err := c.TrackManifest(context.Background(), "1", []Format{FormatFLAC})
			if err != nil {
				t.Fatal(err)
			}
			if m.Preview() != tt.wantPreview || m.PreviewReason != tt.wantReason {
				t.Errorf("preview = %v %q", m.Preview(), m.PreviewReason)
			}
			if !m.ExpiresAt.Equal(tt.wantExpiry) {
				t.Errorf("ExpiresAt = %s, want %s", m.ExpiresAt, tt.wantExpiry)
			}
			if m.SampleRate != tt.wantRate || m.Duration != tt.wantDur {
				t.Errorf("rate %d dur %s, want %d %s", m.SampleRate, m.Duration, tt.wantRate, tt.wantDur)
			}
			if (m.TrackNormalization != nil) != tt.wantNorm {
				t.Errorf("normalization = %v", m.TrackNormalization)
			}
		})
	}
}

func TestTrackManifestRejects(t *testing.T) {
	secretURI := "https://im-cf.manifest.tidal.com/1/manifests/abc.mpd?Expires=1&Signature=SECRETSIG"
	good := buildMPD(mpdOpts{query: "x=1"})
	tests := []struct {
		name    string
		id      string
		formats []Format
		reply   *reply
		is      error
		wantSys string
	}{
		{name: "drmData present", reply: &reply{status: 200, body: manifestDoc(t, map[string]any{"uri": dataURI(good),
			"drmData": map[string]any{"drmSystem": "WIDEVINE", "licenseUrl": "https://lic.example/x"}})}, is: ErrDRMProtected, wantSys: "WIDEVINE"},
		{name: "ContentProtection on the MPD", reply: &reply{status: 200, body: manifestDoc(t, map[string]any{"uri": dataURI(buildMPD(mpdOpts{protection: "mpd"}))})}, is: ErrDRMProtected},
		{name: "ContentProtection on the adaptation set", reply: &reply{status: 200, body: manifestDoc(t, map[string]any{"uri": dataURI(buildMPD(mpdOpts{protection: "set"}))})}, is: ErrDRMProtected},
		{name: "ContentProtection on the representation", reply: &reply{status: 200, body: manifestDoc(t, map[string]any{"uri": dataURI(buildMPD(mpdOpts{protection: "rep"}))})}, is: ErrDRMProtected},
		{name: "https URI instead of data", reply: &reply{status: 200, body: manifestDoc(t, map[string]any{"uri": secretURI})}, is: ErrBadManifest},
		{name: "data URI without base64", reply: &reply{status: 200, body: manifestDoc(t, map[string]any{"uri": "data:application/dash+xml,<MPD/>"})}, is: ErrBadManifest},
		{name: "data URI without comma", reply: &reply{status: 200, body: manifestDoc(t, map[string]any{"uri": "data:application/dash+xml;base64"})}, is: ErrBadManifest},
		{name: "invalid base64", reply: &reply{status: 200, body: manifestDoc(t, map[string]any{"uri": "data:x;base64,!!!SECRETSIG"})}, is: ErrBadManifest},
		{name: "oversized manifest", reply: &reply{status: 200, body: manifestDoc(t, map[string]any{"uri": "data:x;base64," + strings.Repeat("A", 1<<21)})}, is: ErrBadManifest},
		{name: "not XML", reply: &reply{status: 200, body: manifestDoc(t, map[string]any{"uri": dataURI("SECRETSIG not xml")})}, is: ErrBadManifest},
		{name: "wrong XML root", reply: &reply{status: 200, body: manifestDoc(t, map[string]any{"uri": dataURI("<HTML/>")})}, is: ErrBadManifest},
		{name: "array data", reply: &reply{status: 200, body: `{"data":[]}`}, is: ErrBadManifest},
		{name: "bad attribute types", reply: &reply{status: 200, body: `{"data":{"id":"1","type":"trackManifests","attributes":{"formats":"FLAC"}}}`}, is: ErrBadManifest},
		{name: "non-numeric ID makes no request", id: "../users/me", is: ErrInvalidResourceID},
		{name: "empty ID", id: "-", is: nil},
		{name: "no formats makes no request", formats: []Format{}},
	}
	for _, code := range []struct {
		code string
		is   error
	}{
		{"CONCURRENT_PLAYBACK", ErrConcurrentPlayback}, {"GEO_RESTRICTED", ErrGeoRestricted},
		{"PURCHASE_REQUIRED", ErrPurchaseRequired}, {"CLIENT_NOT_ENTITLED", ErrClientNotEntitled},
		{"PREREQUISITE_MISSING", ErrPrerequisiteMissing},
	} {
		tests = append(tests, struct {
			name    string
			id      string
			formats []Format
			reply   *reply
			is      error
			wantSys string
		}{name: "403 " + code.code, reply: &reply{status: 403, body: `{"errors":[{"status":"403","code":"` + code.code + `","detail":"nope"}]}`}, is: code.is})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var replies []reply
			if tt.reply != nil {
				replies = append(replies, *tt.reply)
			}
			f := newFakeAPI(t, replies...)
			c, _ := newTestClient(t, f, &stubTokens{token: tokenA})
			id := "330930030"
			switch tt.id {
			case "":
			case "-":
				id = ""
			default:
				id = tt.id
			}
			formats := []Format{FormatFLAC}
			if tt.formats != nil {
				formats = tt.formats
			}

			_, err := c.TrackManifest(context.Background(), id, formats)

			if err == nil {
				t.Fatal("TrackManifest succeeded, want an error")
			}
			if tt.is != nil && !errors.Is(err, tt.is) {
				t.Errorf("err = %v, want %v", err, tt.is)
			}
			if tt.wantSys != "" {
				var de *DRMError
				if !errors.As(err, &de) || de.System != tt.wantSys || !strings.Contains(err.Error(), tt.wantSys) {
					t.Errorf("err = %#v, want DRMError with system %s", err, tt.wantSys)
				}
			}
			if tt.name == "oversized manifest" && !strings.Contains(err.Error(), "too large") {
				t.Errorf("oversized manifest err = %v, want the size cap to reject it", err)
			}
			if strings.Contains(err.Error(), "SECRETSIG") || strings.Contains(err.Error(), "im-cf.manifest") || strings.Contains(err.Error(), "lic.example") {
				t.Errorf("error leaks manifest data: %v", err)
			}
			if tt.reply == nil && len(f.requests()) != 0 {
				t.Errorf("made %d requests, want none", len(f.requests()))
			}
		})
	}
}

func TestManifestNeverPrintsMPD(t *testing.T) {
	m := Manifest{TrackID: "1", Formats: []Format{FormatFLAC}, Presentation: PresentationFull, SampleRate: 44100, MPD: []byte("Signature=SECRETSIG")}
	var logs bytes.Buffer
	slog.New(slog.NewTextHandler(&logs, nil)).Info("resolved", "manifest", m)
	for name, out := range map[string]string{
		"%v": fmt.Sprintf("%v", m), "%+v": fmt.Sprintf("%+v", m), "%#v": fmt.Sprintf("%#v", m), "%s": fmt.Sprint(m), "slog": logs.String(),
	} {
		if strings.Contains(out, "SECRETSIG") {
			t.Errorf("%s output leaks the MPD: %s", name, out)
		}
		if !strings.Contains(out, "44100") {
			t.Errorf("%s output lacks the summary: %s", name, out)
		}
	}
	if !strings.Contains(logs.String(), "manifest.track_id=1") {
		t.Errorf("slog group missing track_id: %s", logs.String())
	}
}

func TestDRMErrorMessage(t *testing.T) {
	if got := (&DRMError{TrackID: "7"}).Error(); got != "tidal: track 7 is DRM protected" {
		t.Errorf("Error() = %q", got)
	}
	if !errors.Is(&DRMError{}, ErrDRMProtected) || errors.Is(&DRMError{}, ErrBadManifest) {
		t.Error("DRMError.Is is wrong")
	}
}

func TestParseISODuration(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"PT29.994S", 29994 * time.Millisecond, false},
		{"PT4M10S", 4*time.Minute + 10*time.Second, false},
		{"PT1H", time.Hour, false},
		{"P1DT1S", 24*time.Hour + time.Second, false},
		{"", 0, true},
		{"P", 0, true},
		{"PT", 0, false},
		{"PTS", 0, true},
		{"PT5X", 0, true},
		{"P5S", 0, true}, // seconds are only valid after T
		{"PT1.2.3S", 0, true},
		{"PT5", 0, true},
	}
	for _, tt := range tests {
		got, err := parseISODuration(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("parseISODuration(%q) = %s, %v; want %s, err=%v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestPolicyExpiry(t *testing.T) {
	enc := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	tests := []struct {
		name string
		in   string
		want int64
		ok   bool
	}{
		{"empty", "", 0, false},
		{"bad base64", "***", 0, false},
		{"not JSON", enc("nope"), 0, false},
		{"no statements", enc(`{"Statement":[]}`), 0, false},
		{"earliest of two", enc(`{"Statement":[{"Condition":{"DateLessThan":{"AWS:EpochTime":200}}},{"Condition":{"DateLessThan":{"AWS:EpochTime":100}}}]}`), 100, true},
	}
	for _, tt := range tests {
		got, ok := policyExpiry(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("%s: policyExpiry = %d, %v; want %d, %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}

func TestNumericID(t *testing.T) {
	for in, want := range map[string]bool{"1": true, "330930030": true, "": false, "12a": false, "-1": false, strings.Repeat("9", 21): false} {
		if numericID(in) != want {
			t.Errorf("numericID(%q) = %v", in, !want)
		}
	}
}
