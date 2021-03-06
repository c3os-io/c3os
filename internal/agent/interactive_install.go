package agent

import (
	"fmt"
	"strings"

	"github.com/c3os-io/c3os/internal/cmd"
	providerConfig "github.com/c3os-io/c3os/internal/provider/config"
	"github.com/c3os-io/c3os/internal/utils"
	config "github.com/c3os-io/c3os/pkg/config"
	"github.com/erikgeiser/promptkit/textinput"
	"github.com/jaypipes/ghw"
	"github.com/mudler/edgevpn/pkg/node"
	"github.com/mudler/yip/pkg/schema"
	"github.com/pterm/pterm"
)

const (
	canBeEmpty = "Unset"
	yesNo      = "[y]es/[N]o"
)

func prompt(prompt, initialValue, placeHolder string, canBeEmpty, hidden bool) (string, error) {
	input := textinput.New(prompt)
	input.InitialValue = initialValue
	input.Placeholder = placeHolder
	if canBeEmpty {
		input.Validate = func(s string) bool { return true }
	}
	input.Hidden = hidden

	return input.RunPrompt()
}

func isYes(s string) bool {
	i := strings.ToLower(s)
	if i == "y" || i == "yes" {
		return true
	}
	return false
}

const (
	_ = 1 << (10 * iota)
	KiB
	MiB
	GiB
	TiB
)

func InteractiveInstall(spawnShell bool) error {
	cmd.PrintBranding(DefaultBanner)
	pterm.DefaultBox.WithTitle("Installation").WithTitleBottomRight().WithRightPadding(0).WithBottomPadding(0).Println(
		`Interactive installation. Documentation is available at https://docs.c3os.io.`)

	disks := []string{}
	maxSize := float64(0)
	preferedDevice := "/dev/sda"

	block, err := ghw.Block()
	if err == nil {
		for _, disk := range block.Disks {
			size := float64(disk.SizeBytes) / float64(GiB)
			if size > maxSize {
				maxSize = size
				preferedDevice = "/dev/" + disk.Name
			}
			disks = append(disks, fmt.Sprintf("/dev/%s: %s (%.2f GiB) ", disk.Name, disk.Model, float64(disk.SizeBytes)/float64(GiB)))
		}
	}

	pterm.Info.Println("Available Disks:")
	for _, d := range disks {
		pterm.Info.Println(" " + d)
	}
	var networkToken string

	device, err := prompt("What's the target install device?", preferedDevice, "Cannot be empty", false, false)
	if err != nil {
		return err
	}

	userName, err := prompt("User to setup", "c3os", canBeEmpty, true, false)
	if err != nil {
		return err
	}

	userPassword, err := prompt("Password", "", canBeEmpty, true, true)
	if err != nil {
		return err
	}

	if userPassword == "" {
		userPassword = "!"
	}

	sshUsername, err := prompt("Username to grant SSH access to (github/gitlab supported)", "github:someuser", canBeEmpty, true, false)
	if err != nil {
		return err
	}

	sshPubkey, err := prompt("SSH pubkey", "github:username", canBeEmpty, true, false)
	if err != nil {
		return err
	}

	k3sAuto, err := prompt("Do you want to enable k3s automated setup? (requires multiple nodes)", "n", yesNo, true, false)
	if err != nil {
		return err
	}

	if isYes(k3sAuto) {
		hasNetworkToken, err := prompt("Do you have a network token already?", "n", yesNo, true, false)
		if err != nil {
			return err
		}

		if isYes(hasNetworkToken) {
			networkToken, err = prompt("Input network token", "", "", false, true)
			if err != nil {
				return err
			}
		} else {
			networkToken = node.GenerateNewConnectionData().Base64()
		}
	}

	k3sStandalone, err := prompt("Do you want to enable k3s standalone?", "n", yesNo, true, false)
	if err != nil {
		return err
	}

	allGood, err := prompt("Are settings ok?", "n", yesNo, true, false)
	if err != nil {
		return err
	}

	if !isYes(allGood) {
		return InteractiveInstall(spawnShell)
	}

	c := &config.Config{
		Install: &config.Install{
			Device: device,
		},
	}

	providerCfg := providerConfig.Config{
		C3OS: &providerConfig.C3OS{
			NetworkToken: networkToken,
		},
		K3s: providerConfig.K3s{
			Enabled: isYes(k3sStandalone),
		},
	}

	usersToSet := map[string]schema.User{}

	if userName != "" {
		user := schema.User{
			Name:         userName,
			PasswordHash: userPassword,
			Groups:       []string{"admin"},
		}
		if sshUsername != "" {
			user.SSHAuthorizedKeys = append(user.SSHAuthorizedKeys, sshUsername)
		}

		if sshPubkey != "" {
			user.SSHAuthorizedKeys = append(user.SSHAuthorizedKeys, sshPubkey)
		}

		usersToSet = map[string]schema.User{
			userName: user,
		}
	}

	cloudConfig := schema.YipConfig{Name: "Config generated by the installer",
		Stages: map[string][]schema.Stage{config.NetworkStage.String(): {
			{
				Users: usersToSet,
			},
		}}}

	dat, err := config.MergeYAML(cloudConfig, c, providerCfg)
	if err != nil {
		return err
	}

	pterm.Info.Println("Starting installation")

	err = RunInstall(map[string]string{
		"device": device,
		"cc":     config.AddHeader("#node-config", string(dat)),
	})
	if err != nil {
		pterm.Error.Println(err.Error())
	}

	if spawnShell {
		return utils.Shell().Run()
	}
	return err
}
