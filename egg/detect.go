package egg

import (
	"bytes"
	"encoding/json"
)

// LooksLikeEgg reports whether raw JSON is plausibly a Pterodactyl/Pelican egg
// export rather than some other JSON file.
//
// This matters in practice: published egg repositories keep game configuration
// files (world settings, server configs) alongside the eggs themselves, so a
// naive `**/*.json` glob picks up files that were never eggs. Treating those as
// broken eggs would bury real findings in noise, so they are skipped instead.
//
// Detection is deliberately loose — an egg missing `meta.version` is exactly the
// kind of thing the linter should report — so a file counts as an egg if it has
// a PTDL meta block OR enough of the egg-specific field set to be unambiguous.
func LooksLikeEgg(raw []byte) bool {
	var probe struct {
		Meta struct {
			Version string `json:"version"`
		} `json:"meta"`
		Name         string          `json:"name"`
		Author       string          `json:"author"`
		Startup      *string         `json:"startup"`
		Scripts      json.RawMessage `json:"scripts"`
		DockerImages json.RawMessage `json:"docker_images"`
		DockerImage  *string         `json:"docker_image"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}

	if bytes.HasPrefix([]byte(probe.Meta.Version), []byte("PTDL")) {
		return true
	}

	// No usable meta block: require a combination of fields that does not occur
	// together outside an egg export.
	score := 0
	if probe.Startup != nil {
		score++
	}
	if len(probe.Scripts) > 0 {
		score++
	}
	if len(probe.DockerImages) > 0 || probe.DockerImage != nil {
		score++
	}
	if probe.Author != "" && probe.Name != "" {
		score++
	}
	return score >= 3
}
