package tenant

// IDPMode values accepted by MiddlewareConfig.IDPMode and AdminConfig.IDPMode.
const (
	IDPModeAuthentik = "authentik"
	IDPModeZitadel   = "zitadel"
)

// BrowserUIDHeader returns the header name that carries the browser-session
// user identifier for the given IdP mode. Empty mode is treated as authentik
// (matches the MiddlewareConfig empty-default behavior).
func BrowserUIDHeader(mode string) string {
	switch mode {
	case IDPModeZitadel:
		return "X-Auth-Request-User"
	default:
		return "X-Authentik-Uid"
	}
}

// BrowserEmailHeader returns the header that carries the user's email for
// the given IdP mode.
func BrowserEmailHeader(mode string) string {
	switch mode {
	case IDPModeZitadel:
		return "X-Auth-Request-Email"
	default:
		return "X-Authentik-Email"
	}
}

// BrowserGroupsHeader returns the header that carries pipe-separated role
// claims for the given IdP mode.
func BrowserGroupsHeader(mode string) string {
	switch mode {
	case IDPModeZitadel:
		return "X-Auth-Request-Groups"
	default:
		return "X-Authentik-Groups"
	}
}
