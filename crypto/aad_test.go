package crypto

import "testing"

// TestAADConstantsStable pins the byte values of each AAD purpose-string.
// These values are the AEAD binding for every ciphertext in the privacy layer
// — changing any string invalidates the corresponding stored column data and
// MUST be handled by a versioned (-v2) rotation, not an in-place edit.
func TestAADConstantsStable(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"AADMeta", AADMeta, "omur-col-meta-v1"},
		{"AADMetrics", AADMetrics, "omur-col-metrics-v1"},
		{"AADContent", AADContent, "omur-col-content-v1"},
		{"AADEmbeddingsChunks", AADEmbeddingsChunks, "omur-col-embeddings-v1"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Fatalf("AAD drift: %s = %q, want %q", c.name, c.got, c.want)
		}
	}
}
