// Package kms defines the ops-held wrapping interface used by Omur services
// and ships a LocalDevKMS adapter for dev/integration tests. Cloud adapters
// (AWS KMS, GCP KMS, Vault Transit) implement the same interface.
package kms
