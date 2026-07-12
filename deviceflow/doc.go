// Package deviceflow authenticates GitHub Apps and obtains access tokens using
// the OAuth device flow.
//
// A Client requests a device code (GetDeviceCode), polls until the user
// authorizes it (Poll, which calls GetAccessToken), and refreshes an expired
// token (RefreshToken).
package deviceflow
