package model

import "time"

type LogItem struct {
	Timestamp   time.Time `json:"timestamp"`
	Level       string    `json:"level,omitempty"`
	Message     string    `json:"message"`
	Source      string    `json:"source,omitempty"`
	SystemName  string    `json:"systemName,omitempty"`
	Component   string    `json:"component,omitempty"`
	Environment string    `json:"environment,omitempty"`
	Host        string    `json:"host,omitempty"`
	Cluster     string    `json:"cluster,omitempty"`
	Namespace   string    `json:"namespace,omitempty"`
	Pod         string    `json:"pod,omitempty"`
	Container   string    `json:"container,omitempty"`
	TraceID     string    `json:"traceId,omitempty"`
	RequestID   string    `json:"requestId,omitempty"`
	ErrorCode   string    `json:"errorCode,omitempty"`
	Raw         string    `json:"raw"`
}
