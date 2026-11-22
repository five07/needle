package tidal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrNoData is returned when a document has no primary data of the
// requested shape.
var ErrNoData = errors.New("tidal: document has no primary data")

// Document is a JSON:API top-level document.
type Document struct {
	// Data is the primary data: a single resource object, an array of
	// resource objects or identifiers, or null.
	Data json.RawMessage `json:"data"`
	// Included holds related resources requested with include=.
	Included []Resource `json:"included"`
	// Links holds top-level links such as self and next.
	Links Links `json:"links"`
	// Meta holds non-standard top-level information.
	Meta json.RawMessage `json:"meta"`

	index map[resourceKey]int
}

type resourceKey struct{ typ, id string }

// Resource is a JSON:API resource object. Identifier-only entries (as in
// relationship endpoints) decode to a Resource with no attributes.
type Resource struct {
	ID            string                  `json:"id"`
	Type          string                  `json:"type"`
	Attributes    json.RawMessage         `json:"attributes"`
	Relationships map[string]Relationship `json:"relationships"`
	Links         Links                   `json:"links"`
	Meta          json.RawMessage         `json:"meta"`
}

// Relationship is a JSON:API relationship object.
type Relationship struct {
	// Data is the resource linkage: an identifier, an array of identifiers,
	// or null.
	Data  json.RawMessage `json:"data"`
	Links Links           `json:"links"`
	Meta  json.RawMessage `json:"meta"`
}

// Identifier is a JSON:API resource identifier object.
type Identifier struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Meta json.RawMessage `json:"meta"`
}

// Links holds the link members TIDAL uses. Each value may be a string or a
// link object with an href; other members (such as meta) are ignored.
type Links struct {
	Self string
	Next string
}

// UnmarshalJSON accepts string or {"href": ...} link values.
func (l *Links) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	var err error
	if l.Self, err = linkHref(raw["self"]); err != nil {
		return fmt.Errorf("links.self: %w", err)
	}
	if l.Next, err = linkHref(raw["next"]); err != nil {
		return fmt.Errorf("links.next: %w", err)
	}
	return nil
}

func linkHref(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", nil
	}
	var s string
	if raw[0] == '"' {
		err := json.Unmarshal(raw, &s)
		return s, err
	}
	var obj struct {
		Href string `json:"href"`
	}
	err := json.Unmarshal(raw, &obj)
	return obj.Href, err
}

// One returns the primary data as a single resource.
func (d *Document) One() (Resource, error) {
	if isNullOrEmpty(d.Data) || d.Data[0] == '[' {
		return Resource{}, ErrNoData
	}
	var r Resource
	if err := json.Unmarshal(d.Data, &r); err != nil {
		return Resource{}, fmt.Errorf("tidal: decode primary data: %w", err)
	}
	return r, nil
}

// Many returns the primary data as a list of resources. Null data yields an
// empty list; a single resource is an error.
func (d *Document) Many() ([]Resource, error) {
	if isNullOrEmpty(d.Data) {
		return nil, nil
	}
	if d.Data[0] != '[' {
		return nil, ErrNoData
	}
	var rs []Resource
	if err := json.Unmarshal(d.Data, &rs); err != nil {
		return nil, fmt.Errorf("tidal: decode primary data: %w", err)
	}
	return rs, nil
}

// Find returns the included resource with the given type and ID.
func (d *Document) Find(typ, id string) (Resource, bool) {
	if d.index == nil {
		d.index = make(map[resourceKey]int, len(d.Included))
		for i, r := range d.Included {
			d.index[resourceKey{r.Type, r.ID}] = i
		}
	}
	i, ok := d.index[resourceKey{typ, id}]
	if !ok {
		return Resource{}, false
	}
	return d.Included[i], true
}

// Resolve returns the included resources referenced by a relationship's
// linkage, in linkage order, and the identifiers that were not included.
func (d *Document) Resolve(rel Relationship) (found []Resource, missing []Identifier, err error) {
	ids, err := rel.Identifiers()
	if err != nil {
		return nil, nil, err
	}
	for _, id := range ids {
		if r, ok := d.Find(id.Type, id.ID); ok {
			found = append(found, r)
		} else {
			missing = append(missing, id)
		}
	}
	return found, missing, nil
}

// Identifiers returns the relationship linkage as a list. Null or absent
// linkage yields an empty list.
func (r Relationship) Identifiers() ([]Identifier, error) {
	if isNullOrEmpty(r.Data) {
		return nil, nil
	}
	if r.Data[0] == '[' {
		var ids []Identifier
		if err := json.Unmarshal(r.Data, &ids); err != nil {
			return nil, fmt.Errorf("tidal: decode linkage: %w", err)
		}
		return ids, nil
	}
	var id Identifier
	if err := json.Unmarshal(r.Data, &id); err != nil {
		return nil, fmt.Errorf("tidal: decode linkage: %w", err)
	}
	return []Identifier{id}, nil
}

// DecodeAttributes decodes a resource's attributes into T.
func DecodeAttributes[T any](r Resource) (T, error) {
	return decodeRaw[T](r.Attributes, "attributes of "+r.Type+" "+r.ID)
}

// DecodeMeta decodes raw meta (from a resource, identifier, or document)
// into T. Absent meta yields the zero value.
func DecodeMeta[T any](meta json.RawMessage) (T, error) {
	return decodeRaw[T](meta, "meta")
}

func decodeRaw[T any](raw json.RawMessage, what string) (T, error) {
	var v T
	if isNullOrEmpty(raw) {
		return v, nil
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, fmt.Errorf("tidal: decode %s: %w", what, err)
	}
	return v, nil
}

func isNullOrEmpty(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) == 0 || bytes.Equal(raw, []byte("null"))
}
