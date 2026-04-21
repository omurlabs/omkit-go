package crypto

// AAD purpose-string constants bind each per-domain DEK to the data it
// protects. Changing any value invalidates every existing ciphertext encrypted
// with that AAD, so rotations are versioned (e.g. -v2) and migrated separately.
const (
	AADMeta             = "omur-col-meta-v1"
	AADMetrics          = "omur-col-metrics-v1"
	AADContent          = "omur-col-content-v1"
	AADEmbeddingsChunks = "omur-col-embeddings-v1"
)
