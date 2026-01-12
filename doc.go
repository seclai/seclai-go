// Package seclai is the official Seclai Go SDK.
//
// The SDK uses API key authentication via the x-api-key header.
//
// By default, the client reads configuration from environment variables:
//
//   - SECLAI_API_KEY (required)
//   - SECLAI_API_URL (optional; defaults to https://seclai.com)
//
// For most operations, prefer the typed convenience methods on Client.
// Use Client.Do for low-level/escape-hatch requests.
package seclai
