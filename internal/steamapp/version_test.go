package steamapp

import (
	"bufio"
	"strings"
	"testing"
)

func TestParseAndCompareSteamAppManifests(t *testing.T) {
	installed, err := ParseInstalled(bufio.NewScanner(strings.NewReader(`"AppState"
{
	"buildid" "100"
	"InstalledDepots"
	{
		"343051"
		{
			"manifest" "700"
		}
	}
}`)))
	if err != nil {
		t.Fatal(err)
	}
	available, err := ParsePublic(bufio.NewScanner(strings.NewReader(`"343050"
{
	"depots"
	{
		"343051"
		{
			"manifests"
			{
				"public"
				{
					"gid" "701"
				}
			}
		}
		"branches"
		{
			"public"
			{
				"buildid" "101"
			}
		}
	}
}`)), "343050")
	if err != nil {
		t.Fatal(err)
	}
	status, err := Compare(installed, available)
	if err != nil {
		t.Fatal(err)
	}
	if installed.BuildID != "100" || available.BuildID != "101" || !status.UpdateAvailable || status.AvailableVersion != "101" {
		t.Fatalf("installed=%#v available=%#v status=%#v", installed, available, status)
	}
}

func TestCompareRejectsMissingInstalledDepotOnPublicBranch(t *testing.T) {
	_, err := Compare(
		ManifestSnapshot{BuildID: "100", Depots: map[string]string{"343051": "700"}},
		ManifestSnapshot{BuildID: "101", Depots: map[string]string{"343052": "800"}},
	)
	if err == nil || !strings.Contains(err.Error(), "343051") {
		t.Fatalf("Compare() error = %v", err)
	}
}
