package registry

import (
	"encoding/json"

	"github.com/micro/micro/v3/service/logger"
	"github.com/micro/micro/v3/service/router"
)

func (r *registryRouter) logLookupMiss(service string, q router.LookupOptions, source string, candidates []router.Route, refreshBefore uint64) {
	fields := router.LookupDiagnosticFields(q)
	fields["service"] = service
	fields["router_id"] = r.options.Id
	fields["cache_enabled"] = r.options.Cache
	fields["source"] = source
	fields["candidate_count"] = len(candidates)
	fields["refresh_sequence_before_lookup"] = refreshBefore
	fields["refresh_sequence_at_failure"] = r.refreshSequence.Load()
	fields["active_refreshes_at_failure"] = r.activeRefreshes.Load()
	// Summarize the actual unfiltered input, never a second table read.
	type candidate struct {
		UID     string `json:"uid"`
		Address string `json:"address"`
		Network string `json:"network"`
		Gateway string `json:"gateway"`
		Router  string `json:"router"`
		Link    string `json:"link"`
	}
	sample := make([]candidate, 0, 8)
	uidMatches := 0
	for _, rt := range candidates {
		if rt.Metadata["uid"] == q.Uid {
			uidMatches++
		}
		if len(sample) < 8 {
			sample = append(sample, candidate{rt.Metadata["uid"], rt.Address, rt.Network, rt.Gateway, rt.Router, rt.Link})
		}
	}
	fields["target_uid_candidates"] = uidMatches
	fields["candidates_truncated"] = len(candidates) > len(sample)
	encoded, _ := json.Marshal(sample)
	fields["candidates"] = string(encoded)
	logger.Fields(fields).Log(logger.WarnLevel, "Route lookup miss")
}
