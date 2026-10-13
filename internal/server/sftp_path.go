package server

import (
	pathpkg "path"
	"strings"
)

// Windows SFTP absolute paths have a leading slash. Native drive paths must not
// become relative to an Agent's configured SFTP working directory.
func cleanSFTPPath(value string) string {
	slash := strings.ReplaceAll(value, `\`, "/")
	drive := strings.TrimPrefix(slash, "/")
	if len(drive) >= 2 && ((drive[0] >= 'A' && drive[0] <= 'Z') || (drive[0] >= 'a' && drive[0] <= 'z')) && drive[1] == ':' && (len(drive) == 2 || drive[2] == '/') {
		return pathpkg.Clean("/" + strings.ToUpper(drive[:1]) + drive[1:])
	}
	if strings.HasPrefix(slash, "//") {
		return "/" + pathpkg.Clean(slash)
	}
	return pathpkg.Clean(value)
}
