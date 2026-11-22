package tidal

import (
	"context"
	"iter"
	"net/url"
)

// Pages iterates over a cursor-paginated collection, starting at path with
// query and following links.next until it is absent. Each step yields a
// document or an error; iteration ends after an error, when ctx is canceled,
// or when the caller stops ranging.
func (c *Client) Pages(ctx context.Context, path string, query url.Values) iter.Seq2[*Document, error] {
	return func(yield func(*Document, error) bool) {
		next, q := path, query
		for next != "" {
			if err := ctx.Err(); err != nil {
				yield(nil, err)
				return
			}
			doc, err := c.Get(ctx, next, q)
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(doc, nil) {
				return
			}
			// links.next already carries the full query, cursor included.
			next, q = doc.Links.Next, nil
		}
	}
}
