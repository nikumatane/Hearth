package steamapp

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	VersionCheckUnchecked   = "unchecked"
	VersionCheckChecking    = "checking"
	VersionCheckCurrent     = "current"
	VersionCheckAvailable   = "update_available"
	VersionCheckUnavailable = "unavailable"
)

type VersionStatus struct {
	State            string
	AvailableVersion string
	UpdateAvailable  bool
}

type ManifestSnapshot struct {
	BuildID string
	Depots  map[string]string
}

type Scanner interface {
	Scan() bool
	Text() string
	Err() error
}

func ReadBuildID(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return "未知"
	}
	defer file.Close()
	for scanner := bufio.NewScanner(file); scanner.Scan(); {
		fields := quotedFields(strings.TrimSpace(scanner.Text()))
		if len(fields) == 2 && fields[0] == "buildid" && validUint(fields[1], 32) {
			return fields[1]
		}
	}
	return "未知"
}

func ReadInstalled(path string) (ManifestSnapshot, error) {
	file, err := os.Open(path)
	if err != nil {
		return ManifestSnapshot{}, err
	}
	defer file.Close()
	return ParseInstalled(bufio.NewScanner(file))
}

func ReadPublicLog(path, appID string) (ManifestSnapshot, error) {
	file, err := os.Open(path)
	if err != nil {
		return ManifestSnapshot{}, err
	}
	defer file.Close()
	return ParsePublic(bufio.NewScanner(file), appID)
}

func ParseInstalled(scanner Scanner) (ManifestSnapshot, error) {
	snapshot := ManifestSnapshot{Depots: make(map[string]string)}
	stack := make([]string, 0, 8)
	pendingKey := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "", "{":
			if line == "{" && pendingKey != "" {
				stack = append(stack, pendingKey)
				pendingKey = ""
			}
			continue
		case "}":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			pendingKey = ""
			continue
		}
		fields := quotedFields(line)
		switch len(fields) {
		case 1:
			pendingKey = fields[0]
		case 2:
			pendingKey = ""
			if fields[0] == "buildid" && validUint(fields[1], 32) {
				snapshot.BuildID = fields[1]
			}
			if pathEndsWith(stack, "MountedDepots") && validDepotManifest(fields[0], fields[1]) {
				snapshot.Depots[fields[0]] = fields[1]
				continue
			}
			if fields[0] == "manifest" && len(stack) >= 2 && stack[len(stack)-2] == "InstalledDepots" &&
				validDepotManifest(stack[len(stack)-1], fields[1]) {
				snapshot.Depots[stack[len(stack)-1]] = fields[1]
			}
		default:
			pendingKey = ""
		}
	}
	if err := scanner.Err(); err != nil {
		return ManifestSnapshot{}, err
	}
	if snapshot.BuildID == "" {
		return ManifestSnapshot{}, errors.New("installed build ID was not present in app manifest")
	}
	if len(snapshot.Depots) == 0 {
		return ManifestSnapshot{}, errors.New("installed depot manifests were not present in app manifest")
	}
	return snapshot, nil
}

func ParsePublic(scanner Scanner, appID string) (ManifestSnapshot, error) {
	snapshot := ManifestSnapshot{Depots: make(map[string]string)}
	stack := make([]string, 0, 10)
	pendingKey := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "", "{":
			if line == "{" && pendingKey != "" {
				stack = append(stack, pendingKey)
				pendingKey = ""
			}
			continue
		case "}":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			pendingKey = ""
			continue
		}
		fields := quotedFields(line)
		switch len(fields) {
		case 1:
			pendingKey = fields[0]
		case 2:
			pendingKey = ""
			if fields[0] == "buildid" && len(stack) >= 4 && pathEndsWith(stack, "depots", "branches", "public") &&
				stack[len(stack)-4] == appID && validUint(fields[1], 32) {
				snapshot.BuildID = fields[1]
				continue
			}
			if fields[0] != "gid" || len(stack) < 5 || !pathEndsWith(stack, "manifests", "public") {
				continue
			}
			depotID := stack[len(stack)-3]
			if stack[len(stack)-5] == appID && stack[len(stack)-4] == "depots" && validDepotManifest(depotID, fields[1]) {
				snapshot.Depots[depotID] = fields[1]
			}
		default:
			pendingKey = ""
		}
	}
	if err := scanner.Err(); err != nil {
		return ManifestSnapshot{}, err
	}
	if snapshot.BuildID == "" {
		return ManifestSnapshot{}, errors.New("public build ID was not present in SteamCMD output")
	}
	if len(snapshot.Depots) == 0 {
		return ManifestSnapshot{}, errors.New("public depot manifests were not present in SteamCMD output")
	}
	return snapshot, nil
}

func Compare(installed, available ManifestSnapshot) (VersionStatus, error) {
	if len(installed.Depots) == 0 {
		return VersionStatus{}, errors.New("no installed depot manifests to compare")
	}
	for depotID, installedManifest := range installed.Depots {
		availableManifest, ok := available.Depots[depotID]
		if !ok {
			return VersionStatus{}, fmt.Errorf("public manifest for installed depot %s is missing", depotID)
		}
		if availableManifest != installedManifest {
			return VersionStatus{
				State: VersionCheckAvailable, AvailableVersion: available.BuildID, UpdateAvailable: true,
			}, nil
		}
	}
	return VersionStatus{State: VersionCheckCurrent}, nil
}

func validDepotManifest(depotID, manifestID string) bool {
	return validUint(depotID, 32) && validUint(manifestID, 64)
}

func validUint(value string, bits int) bool {
	_, err := strconv.ParseUint(value, 10, bits)
	return err == nil
}

func quotedFields(line string) []string {
	fields := make([]string, 0, 2)
	for {
		start := strings.IndexByte(line, '"')
		if start < 0 {
			break
		}
		line = line[start+1:]
		end := strings.IndexByte(line, '"')
		if end < 0 {
			break
		}
		fields = append(fields, line[:end])
		line = line[end+1:]
	}
	return fields
}

func pathEndsWith(stack []string, suffix ...string) bool {
	if len(stack) < len(suffix) {
		return false
	}
	offset := len(stack) - len(suffix)
	for index := range suffix {
		if stack[offset+index] != suffix[index] {
			return false
		}
	}
	return true
}
