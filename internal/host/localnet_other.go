//go:build !darwin

package host

// bundleIsAdhoc is macOS-only; other platforms never show the local network
// prompt, so nothing is ad-hoc for our purposes.
func bundleIsAdhoc() bool { return false }
