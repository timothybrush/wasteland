package hosted

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gastownhall/wasteland/internal/backend"
	"github.com/gastownhall/wasteland/internal/commons"
	"github.com/gastownhall/wasteland/internal/federation"
	"github.com/gastownhall/wasteland/internal/remote"
)

// ProxyForkRegistrar creates the DoltHub fork and registers the rig using a
// proxy-backed HTTP client rather than a raw API key.
type ProxyForkRegistrar interface {
	EnsureForkAndRegister(client *http.Client, upstream, forkOrg, forkDB, rigHandle, displayName, email string) string
}

// DoltHubProxyForkRegistrar is the production proxy-backed fork registrar.
type DoltHubProxyForkRegistrar struct{}

// EnsureForkAndRegister creates the fork and registers the rig through the
// proxy-authenticated DoltHub client.
func (d *DoltHubProxyForkRegistrar) EnsureForkAndRegister(client *http.Client, upstream, forkOrg, _, rigHandle, displayName, email string) string {
	if client == nil {
		return "no authenticated DoltHub transport available — fork and registration skipped"
	}

	upstreamOrg, upstreamDB, err := federation.ParseUpstream(upstream)
	if err != nil {
		return fmt.Sprintf("invalid upstream %q: %v", upstream, err)
	}

	provider := remote.NewDoltHubProviderWithClient(client)
	if err := provider.Fork(upstreamOrg, upstreamDB, forkOrg); err != nil {
		return fmt.Sprintf("fork failed: %v", err)
	}

	db := backend.NewRemoteDBWithClient(client, upstreamOrg, upstreamDB, forkOrg, upstreamDB, federation.ModePR)
	branch := fmt.Sprintf("wl/register/%s", rigHandle)
	regSQL := commons.BuildRegistrationSQL(rigHandle, forkOrg, displayName, email, "hosted")
	var execErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 3 * time.Second)
		}
		execErr = db.Exec(branch, "", false, regSQL)
		if execErr == nil {
			break
		}
		if !strings.Contains(execErr.Error(), "no such repository") {
			break
		}
		slog.Info("proxy fork registrar: fork not yet available, retrying", "attempt", attempt+1)
	}
	if execErr != nil {
		return fmt.Sprintf("rig registration failed: %v", execErr)
	}

	title := fmt.Sprintf("Register rig: %s", rigHandle)
	body := fmt.Sprintf("Register rig **%s** (%s) in the commons.", rigHandle, displayName)
	if _, err := provider.CreatePR(forkOrg, upstreamOrg, upstreamDB, branch, title, body); err != nil {
		slog.Warn("proxy fork registrar: PR creation failed", "error", err, "handle", rigHandle)
	}
	return ""
}
