// Package buzz owns the pinned Buzz deployment and systemd metadata.
package buzz

import (
	"fmt"
	"strings"

	"github.com/arthur404dev/kothar/internal/inbound"
	"github.com/arthur404dev/kothar/internal/manifest"
)

const (
	Revision        = inbound.BuzzRevision
	PatchSHA256     = "5df36384bb968a440b46c6da0e4846add0bc8efe9d46aa04d96b47a211b1b67f"
	ACPBinarySHA256 = "02079ca1e591dfb888cb1c98bc451fc2a279b6bb9f9010782107c59567989fa9"
	CLIBinarySHA256 = "285d04b1ee3cb37b4231ff648559ab4f81272101d67cd8f4ae5aeb6a1b3d155d"
)

func Adapter() inbound.Adapter { adapter, _ := inbound.Lookup("buzz"); return adapter }

func Unit() []byte {
	return []byte(`[Unit]
Description=Kothar agent %i
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=%I
Group=%I
ExecStart=/usr/local/libexec/kothar/buzz-acp
Restart=always
RestartSec=5s
NoNewPrivileges=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectSystem=strict
ProtectHome=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectControlGroups=yes
LockPersonality=yes
RestrictSUIDSGID=yes
RestrictRealtime=yes
SystemCallArchitectures=native
UMask=0077

[Install]
WantedBy=multi-user.target
`)
}

func DropIn(id string, m *manifest.Manifest) []byte {
	allow := strings.Join(m.Inbound.Options.RespondTo.Pubkeys, ",")
	return []byte(fmt.Sprintf(`[Service]
LoadCredentialEncrypted=%s:/etc/credstore.encrypted/%s
Environment=BUZZ_PRIVATE_KEY_FILE=%%d/%s
Environment=BUZZ_RELAY_URL=%s
Environment=BUZZ_ACP_AGENT_COMMAND=/usr/local/bin/kothar
Environment=BUZZ_ACP_AGENT_ARGS=serve,acp,%s
Environment=BUZZ_ACP_RESPOND_TO=%s
Environment=BUZZ_ACP_RESPOND_TO_ALLOWLIST=%s
Environment=HOME=/var/lib/kothar/agents/%s/home
Environment=XDG_STATE_HOME=/var/lib
Environment=KOTHAR_CONFIG_DIR=/etc/kothar
Environment=KOTHAR_AGENT_STATE_DIR=/var/lib/kothar/agents/%s
Environment=PI_CODING_AGENT_DIR=/var/lib/kothar/agents/%s/engine/pi/agent
WorkingDirectory=/var/lib/kothar/agents/%s
ReadWritePaths=/var/lib/kothar/agents/%s
MemoryMax=%dM
CPUQuota=%d%%
TasksMax=%d
`, m.Inbound.Options.IdentityCredential, m.Inbound.Options.IdentityCredential, m.Inbound.Options.IdentityCredential, m.Inbound.Options.Relay, id,
		m.Inbound.Options.RespondTo.Mode, allow, id, id, id, id, id,
		m.Permissions.Resources.MemoryMaxMB, m.Permissions.Resources.CPUQuotaPercent, m.Permissions.Resources.TasksMax))
}
