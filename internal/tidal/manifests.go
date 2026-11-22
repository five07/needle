package tidal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Format is an audio format accepted by GET /trackManifests.
type Format string

// Formats tidal-tui requests. Dolby Atmos (EAC3_JOC) and HE-AAC are not used.
const (
	FormatAACLC     Format = "AACLC"
	FormatFLAC      Format = "FLAC"
	FormatFLACHires Format = "FLAC_HIRES"
)

// Presentation says whether a manifest is the full track or a preview.
type Presentation string

// Track presentations.
const (
	PresentationFull    Presentation = "FULL"
	PresentationPreview Presentation = "PREVIEW"
)

// PreviewReason explains why only a preview is available.
type PreviewReason string

// Preview reasons.
const (
	PreviewRequiresSubscription     PreviewReason = "FULL_REQUIRES_SUBSCRIPTION"
	PreviewRequiresPurchase         PreviewReason = "FULL_REQUIRES_PURCHASE"
	PreviewRequiresHigherAccessTier PreviewReason = "FULL_REQUIRES_HIGHER_ACCESS_TIER"
)

// ErrDRMProtected means the manifest is DRM protected. tidal-tui does not
// decrypt protected streams (decision-001).
var ErrDRMProtected = errors.New("tidal: track is DRM protected")

// ErrBadManifest means the manifest response could not be understood.
var ErrBadManifest = errors.New("tidal: malformed track manifest")

// DRMError reports a DRM-protected manifest. It matches ErrDRMProtected.
type DRMError struct {
	TrackID string
	// System is the DRM system named by the API (for example WIDEVINE), or
	// "" when protection was only found inside the MPD.
	System string
}

// Error implements error.
func (e *DRMError) Error() string {
	if e.System == "" {
		return fmt.Sprintf("tidal: track %s is DRM protected", e.TrackID)
	}
	return fmt.Sprintf("tidal: track %s is DRM protected (%s)", e.TrackID, truncate(e.System, 40))
}

// Is matches ErrDRMProtected.
func (e *DRMError) Is(target error) bool { return target == ErrDRMProtected }

// Normalization is ReplayGain data for a track or album.
type Normalization struct {
	ReplayGain    float64
	PeakAmplitude float64
}

// Manifest is a playable DASH manifest for one track.
type Manifest struct {
	TrackID string
	// Formats are the formats actually present in the manifest.
	Formats       []Format
	Presentation  Presentation
	PreviewReason PreviewReason
	Hash          string
	// MPD is the DASH manifest. It contains signed segment URLs, so it must
	// never be logged or shown.
	MPD                []byte
	TrackNormalization *Normalization
	AlbumNormalization *Normalization
	// SampleRate is the highest audioSamplingRate in the MPD, or 0.
	SampleRate int
	// BitDepth is the source bit depth from the Representation id (an
	// undocumented "FORMAT,RATE,BITS" form, for example
	// "FLAC_HIRES,192000,24"), or 0 when it cannot be read.
	BitDepth int
	// Duration is the MPD's presentation duration, or 0 if absent.
	Duration  time.Duration
	FetchedAt time.Time
	// ExpiresAt is when the signed segment URLs stop working.
	ExpiresAt time.Time
}

// Preview reports whether only a preview is available. Anything other than
// an explicit FULL counts as a preview, so an unknown value never plays as a
// full track.
func (m Manifest) Preview() bool { return m.Presentation != PresentationFull }

// ValidFor reports whether the signed URLs stay valid for d after now.
func (m Manifest) ValidFor(now time.Time, d time.Duration) bool {
	return now.Add(d).Before(m.ExpiresAt)
}

// String summarizes the manifest without the signed MPD.
func (m Manifest) String() string {
	return fmt.Sprintf("manifest{track=%s formats=%v presentation=%s sample_rate=%d duration=%s}",
		m.TrackID, m.Formats, m.Presentation, m.SampleRate, m.Duration)
}

// GoString keeps %#v from printing the signed MPD.
func (m Manifest) GoString() string { return m.String() }

// LogValue keeps the signed MPD out of structured logs.
func (m Manifest) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("track_id", m.TrackID),
		slog.Any("formats", m.Formats),
		slog.String("presentation", string(m.Presentation)),
		slog.Int("sample_rate", m.SampleRate),
	)
}

const (
	// maxMPD bounds the decoded manifest size.
	maxMPD = 1 << 20
	// fallbackValidity is used when the signed URL expiry cannot be read.
	// Observed validity is 60 minutes.
	fallbackValidity = 50 * time.Minute
)

// TrackManifest fetches the DASH manifest for trackID, offering formats in
// order of preference. It returns a *DRMError for protected manifests and
// wraps ErrBadManifest when the response cannot be decoded.
func (c *Client) TrackManifest(ctx context.Context, trackID string, formats []Format) (Manifest, error) {
	if !numericID(trackID) {
		return Manifest{}, fmt.Errorf("%w: track ID must be numeric", ErrInvalidResourceID)
	}
	if len(formats) == 0 {
		return Manifest{}, errors.New("tidal: at least one format is required")
	}
	q := url.Values{}
	for _, f := range formats {
		q.Add("formats", string(f))
	}
	q.Set("manifestType", "MPEG_DASH")
	q.Set("uriScheme", "DATA")
	q.Set("usage", "PLAYBACK")
	q.Set("adaptive", "false")
	doc, err := c.Get(ctx, "/trackManifests/"+trackID, q)
	if err != nil {
		return Manifest{}, err
	}
	fetched := c.now()
	r, err := doc.One()
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %w", ErrBadManifest, err)
	}
	attrs, err := DecodeAttributes[manifestAttrs](r)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %w", ErrBadManifest, err)
	}
	if attrs.DRMData != nil {
		return Manifest{}, &DRMError{TrackID: trackID, System: attrs.DRMData.DRMSystem}
	}
	mpd, err := decodeDataURI(attrs.URI)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %w", ErrBadManifest, err)
	}
	info, err := inspectMPD(mpd)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %w", ErrBadManifest, err)
	}
	if info.protected {
		return Manifest{}, &DRMError{TrackID: trackID}
	}
	m := Manifest{
		TrackID:            trackID,
		Formats:            attrs.Formats,
		Presentation:       attrs.TrackPresentation,
		PreviewReason:      attrs.PreviewReason,
		Hash:               attrs.Hash,
		MPD:                mpd,
		TrackNormalization: attrs.TrackNorm.value(),
		AlbumNormalization: attrs.AlbumNorm.value(),
		SampleRate:         info.sampleRate,
		BitDepth:           info.bitDepth,
		Duration:           info.duration,
		FetchedAt:          fetched,
		ExpiresAt:          fetched.Add(fallbackValidity),
	}
	// Trust the signed expiry only when it is plausible against the local
	// clock; a skewed clock must not make every manifest look expired.
	if exp, ok := info.expiry(); ok && exp.After(fetched.Add(time.Minute)) {
		m.ExpiresAt = exp
	}
	return m, nil
}

type manifestAttrs struct {
	URI               string        `json:"uri"`
	Formats           []Format      `json:"formats"`
	TrackPresentation Presentation  `json:"trackPresentation"`
	PreviewReason     PreviewReason `json:"previewReason"`
	Hash              string        `json:"hash"`
	DRMData           *struct {
		DRMSystem string `json:"drmSystem"`
	} `json:"drmData"`
	TrackNorm *normAttrs `json:"trackAudioNormalizationData"`
	AlbumNorm *normAttrs `json:"albumAudioNormalizationData"`
}

type normAttrs struct {
	ReplayGain    float64 `json:"replayGain"`
	PeakAmplitude float64 `json:"peakAmplitude"`
}

func (n *normAttrs) value() *Normalization {
	if n == nil {
		return nil
	}
	return &Normalization{ReplayGain: n.ReplayGain, PeakAmplitude: n.PeakAmplitude}
}

func numericID(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// decodeDataURI decodes a base64 data: URI. Error messages never include
// the URI, which may contain signed URLs.
func decodeDataURI(uri string) ([]byte, error) {
	rest, ok := strings.CutPrefix(uri, "data:")
	if !ok {
		return nil, errors.New("manifest uri is not a data URI")
	}
	header, payload, ok := strings.Cut(rest, ",")
	if !ok || !strings.HasSuffix(header, ";base64") {
		return nil, errors.New("manifest data URI is not base64")
	}
	if base64.StdEncoding.DecodedLen(len(payload)) > maxMPD {
		return nil, errors.New("manifest is too large")
	}
	b, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, errors.New("manifest data URI has invalid base64")
	}
	return b, nil
}

type mpdInfo struct {
	protected   bool
	sampleRate  int
	bitDepth    int
	duration    time.Duration
	segmentURLs []string
}

type mpdXML struct {
	XMLName           xml.Name   `xml:"MPD"`
	Duration          string     `xml:"mediaPresentationDuration,attr"`
	ContentProtection []struct{} `xml:"ContentProtection"`
	Periods           []struct {
		AdaptationSets []struct {
			ContentProtection []struct{}   `xml:"ContentProtection"`
			SegmentTemplate   *segmentsXML `xml:"SegmentTemplate"`
			Representations   []struct {
				ID                string       `xml:"id,attr"`
				AudioSamplingRate string       `xml:"audioSamplingRate,attr"`
				ContentProtection []struct{}   `xml:"ContentProtection"`
				SegmentTemplate   *segmentsXML `xml:"SegmentTemplate"`
			} `xml:"Representation"`
		} `xml:"AdaptationSet"`
	} `xml:"Period"`
}

type segmentsXML struct {
	Initialization string `xml:"initialization,attr"`
	Media          string `xml:"media,attr"`
}

func (s *segmentsXML) urls() []string {
	if s == nil {
		return nil
	}
	return []string{s.Initialization, s.Media}
}

func inspectMPD(b []byte) (mpdInfo, error) {
	var doc mpdXML
	if err := xml.Unmarshal(b, &doc); err != nil {
		return mpdInfo{}, errors.New("manifest is not a valid DASH MPD")
	}
	info := mpdInfo{protected: len(doc.ContentProtection) > 0}
	info.duration, _ = parseISODuration(doc.Duration)
	for _, p := range doc.Periods {
		for _, as := range p.AdaptationSets {
			info.protected = info.protected || len(as.ContentProtection) > 0
			info.segmentURLs = append(info.segmentURLs, as.SegmentTemplate.urls()...)
			for _, rep := range as.Representations {
				info.protected = info.protected || len(rep.ContentProtection) > 0
				info.segmentURLs = append(info.segmentURLs, rep.SegmentTemplate.urls()...)
				if sr, err := strconv.Atoi(rep.AudioSamplingRate); err == nil && sr > info.sampleRate {
					info.sampleRate = sr
					info.bitDepth = idBitDepth(rep.ID)
				}
			}
		}
	}
	return info, nil
}

// expiry returns the earliest expiry among the signed segment URLs, from a
// CloudFront canned (Expires) or custom (Policy DateLessThan) signature.
func (i mpdInfo) expiry() (time.Time, bool) {
	var earliest time.Time
	for _, raw := range i.segmentURLs {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		q := u.Query()
		var epoch int64
		if e, err := strconv.ParseInt(q.Get("Expires"), 10, 64); err == nil {
			epoch = e
		} else if e, ok := policyExpiry(q.Get("Policy")); ok {
			epoch = e
		}
		if epoch <= 0 {
			continue
		}
		if t := time.Unix(epoch, 0); earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
	}
	return earliest, !earliest.IsZero()
}

// policyExpiry decodes a CloudFront custom policy (URL-safe base64 with
// '-', '_', '~' substitutions) and returns its DateLessThan epoch.
func policyExpiry(p string) (int64, bool) {
	if p == "" {
		return 0, false
	}
	p = strings.NewReplacer("-", "+", "_", "=", "~", "/").Replace(p)
	b, err := base64.StdEncoding.DecodeString(p)
	if err != nil {
		return 0, false
	}
	var policy struct {
		Statement []struct {
			Condition struct {
				DateLessThan struct {
					Epoch int64 `json:"AWS:EpochTime"`
				} `json:"DateLessThan"`
			} `json:"Condition"`
		} `json:"Statement"`
	}
	if json.Unmarshal(b, &policy) != nil {
		return 0, false
	}
	var min int64
	for _, s := range policy.Statement {
		if e := s.Condition.DateLessThan.Epoch; e > 0 && (min == 0 || e < min) {
			min = e
		}
	}
	return min, min > 0
}

// parseISODuration parses the PnDTnHnMnS subset of ISO 8601 used by DASH.
func parseISODuration(s string) (time.Duration, error) {
	rest, ok := strings.CutPrefix(s, "P")
	if !ok || rest == "" {
		return 0, fmt.Errorf("invalid duration %q", truncate(s, 40))
	}
	var d time.Duration
	inTime := false
	units := map[bool]map[byte]time.Duration{
		false: {'D': 24 * time.Hour},
		true:  {'H': time.Hour, 'M': time.Minute, 'S': time.Second},
	}
	for rest != "" {
		if rest[0] == 'T' {
			inTime, rest = true, rest[1:]
			continue
		}
		i := strings.IndexFunc(rest, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
		if i <= 0 {
			return 0, fmt.Errorf("invalid duration %q", truncate(s, 40))
		}
		unit, ok := units[inTime][rest[i]]
		if !ok {
			return 0, fmt.Errorf("invalid duration %q", truncate(s, 40))
		}
		n, err := strconv.ParseFloat(rest[:i], 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", truncate(s, 40))
		}
		d += time.Duration(n * float64(unit))
		rest = rest[i+1:]
	}
	return d, nil
}

// idBitDepth reads BITS from a "FORMAT,RATE,BITS" Representation id.
func idBitDepth(id string) int {
	parts := strings.Split(id, ",")
	if len(parts) != 3 {
		return 0
	}
	switch bits, err := strconv.Atoi(parts[2]); {
	case err != nil, bits != 16 && bits != 24 && bits != 32:
		return 0
	default:
		return bits
	}
}
