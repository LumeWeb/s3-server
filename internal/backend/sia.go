package backend

import (
	"go.sia.tech/core/types"
	sdk "go.sia.tech/siastorage"
)

// SiaAppMetadata returns the canonical AppMetadata used when registering
// this application with the Sia storage SDK. It is consumed by the backend
// factory, the account client, and the onboarding config endpoint.
func SiaAppMetadata() sdk.AppMetadata {
	return sdk.AppMetadata{
		ID:          types.HashBytes([]byte("s3d")),
		Name:        "Pinner S3 Server",
		Description: "A S3-compatible storage service backed by Sia",
		LogoURL:     "https://pinner.xyz/images/s3-server.png",
		ServiceURL:  "https://github.com/LumeWeb/s3-server",
	}
}
