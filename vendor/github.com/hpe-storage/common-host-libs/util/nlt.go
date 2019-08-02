package util

// Copyright 2019 Hewlett Packard Enterprise Development LP.

const (
	installDirPattern    = "^install_dir\\s*=\\s*(?P<install_dir>.*)"
	nltConf              = "/etc/nlt.conf"
	hpeInstallDir        = "/opt/hpe-storage/"
	nltDefaultInstallDir = "/opt/NimbleStorage"
)

// GetNltHome returns base install directory of NLT or HPE cloud toolkits
func GetNltHome() (installDir string) {
	conf := nltConf
	installDir = nltDefaultInstallDir
	isPresent, _, err := FileExists(nltConf)
	if err != nil || !isPresent {
		// If NLT is not installed, assume common hpe install directory for plugin
		return hpeInstallDir
	}

	lines, err := FileGetStringsWithPattern(conf, installDirPattern)
	if err != nil {
		// Assume default NLT installation directory if install_dir param not present
		return installDir
	}
	if len(lines) > 0 {
		installDir = lines[0]
	}

	isPresent, _, err = FileExists(installDir)
	if err != nil || !isPresent {
		// NLT seems to be not installed, return empty string
		installDir = ""
	}
	return installDir
}
