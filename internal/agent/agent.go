// Package agent models the collectors managed by Arveld.
package agent

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// InstanceUIDSize is the required size of an OpAMP instance UID.
const InstanceUIDSize = 16

// InstanceUID uniquely identifies a running OpAMP agent.
type InstanceUID [InstanceUIDSize]byte

// NewInstanceUID creates an instance UID from its binary representation.
func NewInstanceUID(value []byte) (InstanceUID, error) {
	if len(value) != InstanceUIDSize {
		return InstanceUID{}, fmt.Errorf(
			"instance UID has %d bytes, want %d",
			len(value),
			InstanceUIDSize,
		)
	}

	var uid InstanceUID
	copy(uid[:], value)

	return uid, nil
}

// ParseInstanceUID creates an instance UID from its hexadecimal text representation.
func ParseInstanceUID(value string) (InstanceUID, error) {
	const canonicalTextSize = InstanceUIDSize*2 + 4
	if len(value) != canonicalTextSize {
		return InstanceUID{}, fmt.Errorf(
			"instance UID text has %d bytes, want %d",
			len(value),
			canonicalTextSize,
		)
	}
	if value[8] != '-' ||
		value[13] != '-' ||
		value[18] != '-' ||
		value[23] != '-' {
		return InstanceUID{}, errors.New(
			"instance UID text has invalid separator positions",
		)
	}

	decoded, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	if err != nil {
		return InstanceUID{}, fmt.Errorf("decode instance UID: %w", err)
	}

	return NewInstanceUID(decoded)
}

// String returns the canonical UUID representation of uid.
func (uid InstanceUID) String() string {
	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		uid[0:4],
		uid[4:6],
		uid[6:8],
		uid[8:10],
		uid[10:16],
	)
}

// Agent describes a collector managed by Arveld.
type Agent struct {
	InstanceUID InstanceUID
	Hostname    *string
	Version     *string
	Connected   bool
	LastSeenAt  *time.Time
}
