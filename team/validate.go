package team

import "fmt"

// ValidateUpdate tests whether the given changes to a team are OK
func ValidateUpdate(before *Team, after *Team, me *Person) error {
	// validate the team UUID didn't change
	// team name can't change
	// can't change the email for a given key
	// how to handle email address changes? deny them?
	// don't allow people to be arbitrarily added (I think)
	// email address appears twice
	// fingerprint appears twice
	// I can't remove myself from a team
	// I can't demote myself as an admin (another admin must promote)
	// factor out before/after tests from API?
	// validate that there's still a team admin
	// protect against replay attacks: prevent someone from uploading an old (signed) version of the file
	// signing key's fingerprint missing from roster
	// signing key isn't listed in roster
	// signing key listed in roster but not an admin

	if err := validateTeamUUID(before, after); err != nil {
		return err
	}

	if err := validateTeamNameCantChange(before, after); err != nil {
		return err
	}

	if err := validateIAmAdmin(before, after); err != nil {
		return err
	}

	return nil
}

func validateTeamUUID(before, after *Team) error {
	if before.UUID != after.UUID {
		return fmt.Errorf("can't change team UUID")
	}
	return nil
}

func validateTeamNameCantChange(before, after *Team) error {
	if before.Name != after.Name {
		return fmt.Errorf("can't change team name")
	}
	return nil
}

func validateIAmAdmin(before, after *Team, me *Person) error {
	return fmt.Errorf("not implemented")
}
