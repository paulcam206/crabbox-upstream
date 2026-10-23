package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

type separateLoginTestProvider struct{ Provider }

func (separateLoginTestProvider) Spec() ProviderSpec {
	return ProviderSpec{Name: "separate-login-test", DesktopLoginPasswordSeparate: true}
}

func TestWriteVNCCredentialsKeepsSeparateWindowsLoginSecretPrivate(t *testing.T) {
	RegisterProvider(separateLoginTestProvider{})
	t.Cleanup(func() { delete(providerRegistry, "separate-login-test") })
	var output bytes.Buffer
	writeVNCCredentials(
		&output,
		Config{Provider: "separate-login-test"},
		SSHTarget{TargetOS: targetWindows, User: "crabbox"},
		vncEndpoint{Managed: true},
		false,
		rfbCredentials{Username: "crabbox", Password: "vnc-only-secret"},
	)
	got := output.String()
	if !strings.Contains(got, "password: vnc-only-secret") {
		t.Fatalf("missing VNC password: %q", got)
	}
	if strings.Contains(got, "windows password: vnc-only-secret") {
		t.Fatalf("VNC secret was advertised as the Windows login password: %q", got)
	}
	if !strings.Contains(got, "windows username: crabbox") {
		t.Fatalf("missing windows username: %q", got)
	}
}

func TestWindowsDesktopVideoCommandDoesNotRequireCredentialEscrow(t *testing.T) {
	script := windowsDesktopVideoRemoteCommand("task", `C:\ProgramData\crabbox\capture.ps1`, `C:\ProgramData\crabbox\out`, `C:\ProgramData\crabbox\out.zip`, 5*time.Second)
	if strings.Contains(script, "windows.password") || strings.Contains(script, "credential file is missing") {
		t.Fatalf("video recording must not require the Windows credential escrow file: %s", script)
	}
}

func TestWindowsDesktopTerminalScopesMsysConversionToSuppliedCommand(t *testing.T) {
	interactive, err := desktopTerminalCommand(SSHTarget{TargetOS: targetWindows, WindowsMode: windowsModeNormal}, nil, desktopTerminalOptions{Sixel: true})
	if err != nil {
		t.Fatal(err)
	}
	shell := interactive[len(interactive)-1]
	if strings.Contains(shell, "MSYS") || !strings.Contains(shell, "TERM=xterm-256color") {
		t.Fatalf("interactive shell must keep MSYS conversion and sixel env: %q", shell)
	}
	command, err := desktopTerminalCommand(SSHTarget{TargetOS: targetWindows, WindowsMode: windowsModeNormal}, []string{"git.exe", "status"}, desktopTerminalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(command[len(command)-1], "MSYS_NO_PATHCONV=1") {
		t.Fatalf("supplied command must keep switch preservation: %q", command)
	}
}

func TestManagedWindowsDesktopBootstrapReconcilesPreinstalledTightVNC(t *testing.T) {
	script := managedWindowsDesktopBootstrapPowerShell("crabbox")
	removal := strings.Index(script, "msiexec.exe")
	recipe := strings.Index(script, "tvnserver.exe")
	if removal < 0 || recipe < 0 {
		t.Fatalf("bootstrap must remove a preinstalled TightVNC before the install recipe")
	}
	if !strings.Contains(script, "$vncPasswordChanged") {
		t.Fatalf("bootstrap must only reinstall when the managed secret changed")
	}
}
