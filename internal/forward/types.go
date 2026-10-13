package forward

import protocoltypes "github.com/qinyongliang/gosshd-bastion/internal/protocol/types"

// Aliases preserve existing callers while keeping DTO ownership in the protocol layer.
type HTTPConfig = protocoltypes.HTTPConfig
type HTTPListener = protocoltypes.HTTPListener
type ServerBlock = protocoltypes.ServerBlock
type Location = protocoltypes.Location
type Event = protocoltypes.Event
type LogFilter = protocoltypes.LogFilter
type Stats = protocoltypes.Stats
type SourceStats = protocoltypes.SourceStats
type RuntimeState = protocoltypes.RuntimeState
type TrafficBucket = protocoltypes.TrafficBucket
