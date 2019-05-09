package fk

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"

	"github.com/fluidkeys/fluidkeys/out"
	"github.com/fluidkeys/fluidkeys/team"
	"github.com/fluidkeys/fluidkeys/ui"
)

func teamEdit() exitCode {
	allMemberships, err := user.Memberships()
	if err != nil {
		out.Print(ui.FormatFailure("Failed to list teams", nil, err))
		return 1
	}

	adminMemberships := filterByAdmin(allMemberships)

	switch len(adminMemberships) {
	case 0:
		out.Print(ui.FormatFailure("You aren't an admin of any teams", nil, nil))
		return 1

	case 1:
		return doEditTeam(adminMemberships[0].Team, adminMemberships[0].Me)

	default:
		out.Print(ui.FormatFailure("Choosing from multiple teams not implemented", nil, nil))
		return 1
	}
}

func doEditTeam(myTeam team.Team, me team.Person) exitCode {
	printHeader("Edit team " + myTeam.Name)

	existingRoster, _ := myTeam.Roster()
	// TODO: validate signature

	tmpfile, err := writeRosterToTempfile(existingRoster)
	if err != nil {
		out.Print(ui.FormatFailure("Error writing team roster to temporary file", nil, err))
		return 1
	}
	defer os.Remove(tmpfile)

	err = runEditor(tmpfile)
	if err != nil {
		out.Print(ui.FormatFailure("failed to open editor for text file", nil, err))
		return 1
	}

	newRoster, err := ioutil.ReadFile(tmpfile)
	if err != nil {
		out.Print(ui.FormatFailure("Error reading temp file", nil, err))
		return 1
	}

	updatedTeam, err := team.Load(string(newRoster), "")
	if err != nil {
		out.Print(ui.FormatFailure("Problem with new team roster", nil, err))
		return 1
	}

	// TODO: validation!
	if err := team.ValidateUpdate(myTeam, updatedTeam); err != nil {
		out.Print(ui.FormatFailure("Problem with new team roster", nil, err))
		return 1
	}

	if err := promptAndSignAndUploadRoster(*updatedTeam, me.Fingerprint); err != nil {
		if err != errUserDeclinedToSign {
			out.Print(ui.FormatFailure("Failed to sign and upload roster", nil, err))
		}
		return 1
	}

	return 0
}

func writeRosterToTempfile(roster string) (tmpFilename string, err error) {
	tmpfile, err := ioutil.TempFile("", "roster.toml_")
	if err != nil {
		return "", fmt.Errorf("error creating temp file: %v", err)
	}

	if _, err := tmpfile.Write([]byte(roster)); err != nil {
		return "", fmt.Errorf("error writing %s: %v", tmpfile.Name(), err)
	}
	if err := tmpfile.Close(); err != nil {
		return "", fmt.Errorf("error closing %s: %v", tmpfile.Name(), err)
	}
	return tmpfile.Name(), nil
}

func runEditor(filename string) error {
	editor := "vim" // TODO
	editorAbs, err := exec.LookPath(editor)
	if err != nil {
		return fmt.Errorf("failed to find editor `%s`: %v", editor, err)
	}
	out.Print("Running " + editorAbs + " " + filename + "\n")

	cmd := exec.Command(editorAbs, filename)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start editor: %v", err)
	}

	if err = cmd.Wait(); err != nil {
		return fmt.Errorf("failed to start editor: %v", err)
	}
	return nil
}
