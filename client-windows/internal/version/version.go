package version

// GitHubOwner and GitHubRepo are compile-time constants, not ldflags, so the
// server can declare which release to install but can never redirect the
// download to a different origin.
const (
	GitHubOwner = "pre"
	GitHubRepo  = "rootaika"
)

// Version is the running client version. It is injected at build time with
//
//	-X rootaika/client-windows/internal/version.Version=v1.2.0
//
// and defaults to "dev" for local builds.
var Version = "dev"
