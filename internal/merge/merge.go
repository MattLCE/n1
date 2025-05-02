// Package merge implements the merge algorithm for n1 vaults.
// It provides functionality for merging concurrent updates from multiple replicas
// while preserving the append-only, content-addressed data model.
package merge

import (
	"errors"
	"fmt"
	"time"
)

// Common errors returned by the merge package.
var (
	ErrInvalidEvent     = errors.New("invalid event")
	ErrCyclicDependency = errors.New("cyclic dependency detected")
)

// UUID represents a universally unique identifier.
type UUID [16]byte

// String returns a string representation of the UUID.
func (id UUID) String() string {
	return fmt.Sprintf("%x", id[:])
}

// EventType represents the type of an event.
type EventType int

const (
	// EventTypePut represents a Put operation.
	EventTypePut EventType = iota
	// EventTypeDelete represents a Delete operation.
	EventTypeDelete
	// EventTypeMerge represents a Merge operation.
	EventTypeMerge
)

// String returns a string representation of the event type.
func (t EventType) String() string {
	switch t {
	case EventTypePut:
		return "Put"
	case EventTypeDelete:
		return "Delete"
	case EventTypeMerge:
		return "Merge"
	default:
		return "Unknown"
	}
}

// Operation represents an operation performed on a key.
type Operation interface {
	// Type returns the type of the operation.
	Type() EventType
	// Key returns the key affected by the operation.
	Key() string
}

// PutOperation represents a Put operation.
type PutOperation struct {
	key      string
	value    []byte
	metadata map[string]string
}

// NewPutOperation creates a new Put operation.
func NewPutOperation(key string, value []byte, metadata map[string]string) *PutOperation {
	return &PutOperation{
		key:      key,
		value:    value,
		metadata: metadata,
	}
}

// Type returns the type of the operation.
func (o *PutOperation) Type() EventType {
	return EventTypePut
}

// Key returns the key affected by the operation.
func (o *PutOperation) Key() string {
	return o.key
}

// Value returns the value of the operation.
func (o *PutOperation) Value() []byte {
	return o.value
}

// Metadata returns the metadata of the operation.
func (o *PutOperation) Metadata() map[string]string {
	return o.metadata
}

// DeleteOperation represents a Delete operation.
type DeleteOperation struct {
	key    string
	reason string
}

// NewDeleteOperation creates a new Delete operation.
func NewDeleteOperation(key string, reason string) *DeleteOperation {
	return &DeleteOperation{
		key:    key,
		reason: reason,
	}
}

// Type returns the type of the operation.
func (o *DeleteOperation) Type() EventType {
	return EventTypeDelete
}

// Key returns the key affected by the operation.
func (o *DeleteOperation) Key() string {
	return o.key
}

// Reason returns the reason for the deletion.
func (o *DeleteOperation) Reason() string {
	return o.reason
}

// MergeOperation represents a Merge operation.
type MergeOperation struct {
	key        string
	eventIDs   []UUID
	resolution string
}

// NewMergeOperation creates a new Merge operation.
func NewMergeOperation(key string, eventIDs []UUID, resolution string) *MergeOperation {
	return &MergeOperation{
		key:        key,
		eventIDs:   eventIDs,
		resolution: resolution,
	}
}

// Type returns the type of the operation.
func (o *MergeOperation) Type() EventType {
	return EventTypeMerge
}

// Key returns the key affected by the operation.
func (o *MergeOperation) Key() string {
	return o.key
}

// EventIDs returns the IDs of the events being merged.
func (o *MergeOperation) EventIDs() []UUID {
	return o.eventIDs
}

// Resolution returns the resolution of the merge.
func (o *MergeOperation) Resolution() string {
	return o.resolution
}

// Event represents an event in the event log.
type Event struct {
	// ID is the unique identifier of the event.
	ID UUID
	// ReplicaID is the ID of the replica that created the event.
	ReplicaID UUID
	// LamportClock is the logical timestamp of the event.
	LamportClock uint64
	// ParentIDs are the IDs of the parent events.
	ParentIDs []UUID
	// Operation is the operation performed by the event.
	Operation Operation
	// Timestamp is the wall-clock time of the event.
	Timestamp time.Time
}

// NewEvent creates a new event.
func NewEvent(id UUID, replicaID UUID, lamportClock uint64, parentIDs []UUID, operation Operation, timestamp time.Time) *Event {
	return &Event{
		ID:           id,
		ReplicaID:    replicaID,
		LamportClock: lamportClock,
		ParentIDs:    parentIDs,
		Operation:    operation,
		Timestamp:    timestamp,
	}
}

// EventGraph represents a directed acyclic graph of events.
type EventGraph struct {
	events      map[UUID]*Event
	childMap    map[UUID][]UUID
	keyToEvents map[string][]UUID
}

// NewEventGraph creates a new event graph.
func NewEventGraph() *EventGraph {
	return &EventGraph{
		events:      make(map[UUID]*Event),
		childMap:    make(map[UUID][]UUID),
		keyToEvents: make(map[string][]UUID),
	}
}

// AddEvent adds an event to the graph.
func (g *EventGraph) AddEvent(event *Event) error {
	// Check if the event already exists
	if _, exists := g.events[event.ID]; exists {
		return nil // Already added
	}

	// Add the event
	g.events[event.ID] = event

	// Update the child map
	for _, parentID := range event.ParentIDs {
		g.childMap[parentID] = append(g.childMap[parentID], event.ID)
	}

	// Update the key-to-events map
	key := event.Operation.Key()
	g.keyToEvents[key] = append(g.keyToEvents[key], event.ID)

	return nil
}

// GetEvent gets an event by its ID.
func (g *EventGraph) GetEvent(id UUID) (*Event, error) {
	event, exists := g.events[id]
	if !exists {
		return nil, ErrInvalidEvent
	}
	return event, nil
}

// GetChildren gets the children of an event.
func (g *EventGraph) GetChildren(id UUID) ([]*Event, error) {
	childIDs, exists := g.childMap[id]
	if !exists {
		return nil, ErrInvalidEvent
	}

	children := make([]*Event, 0, len(childIDs))
	for _, childID := range childIDs {
		child, err := g.GetEvent(childID)
		if err != nil {
			return nil, err
		}
		children = append(children, child)
	}

	return children, nil
}

// GetEventsByKey gets all events affecting a key.
func (g *EventGraph) GetEventsByKey(key string) ([]*Event, error) {
	eventIDs, exists := g.keyToEvents[key]
	if !exists {
		return []*Event{}, nil
	}

	events := make([]*Event, 0, len(eventIDs))
	for _, eventID := range eventIDs {
		event, err := g.GetEvent(eventID)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	return events, nil
}

// TopologicalSort performs a topological sort of the events.
func (g *EventGraph) TopologicalSort() ([]*Event, error) {
	// Create a map of in-degrees
	inDegree := make(map[UUID]int)
	for id := range g.events {
		inDegree[id] = 0
	}

	// Calculate in-degrees
	for _, event := range g.events {
		for _, parentID := range event.ParentIDs {
			if _, exists := g.events[parentID]; exists {
				inDegree[parentID]++
			}
		}
	}

	// Find roots (events with no parents in the graph)
	var queue []UUID
	for id, event := range g.events {
		if len(event.ParentIDs) == 0 {
			queue = append(queue, id)
		}
	}

	// Perform topological sort
	var sorted []*Event
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]

		event, err := g.GetEvent(id)
		if err != nil {
			return nil, err
		}
		sorted = append(sorted, event)

		childIDs, exists := g.childMap[id]
		if !exists {
			continue
		}

		for _, childID := range childIDs {
			inDegree[childID]--
			if inDegree[childID] == 0 {
				queue = append(queue, childID)
			}
		}
	}

	// Check for cycles
	if len(sorted) != len(g.events) {
		return nil, ErrCyclicDependency
	}

	// Sort concurrent events by Lamport clock and replica ID
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			// If neither event depends on the other, they are concurrent
			if !g.isDependentOn(sorted[i].ID, sorted[j].ID) && !g.isDependentOn(sorted[j].ID, sorted[i].ID) {
				// Order by Lamport clock
				if sorted[i].LamportClock > sorted[j].LamportClock {
					sorted[i], sorted[j] = sorted[j], sorted[i]
				} else if sorted[i].LamportClock == sorted[j].LamportClock {
					// If Lamport clocks are equal, order by replica ID
					if compareUUIDs(sorted[i].ReplicaID, sorted[j].ReplicaID) > 0 {
						sorted[i], sorted[j] = sorted[j], sorted[i]
					}
				}
			}
		}
	}

	return sorted, nil
}

// isDependentOn checks if event with ID a depends on event with ID b.
func (g *EventGraph) isDependentOn(a, b UUID) bool {
	visited := make(map[UUID]bool)
	return g.isDependentOnRecursive(a, b, visited)
}

// isDependentOnRecursive is a recursive helper for isDependentOn.
func (g *EventGraph) isDependentOnRecursive(current, target UUID, visited map[UUID]bool) bool {
	if current == target {
		return true
	}

	if visited[current] {
		return false
	}
	visited[current] = true

	event, exists := g.events[current]
	if !exists {
		return false
	}

	for _, parentID := range event.ParentIDs {
		if g.isDependentOnRecursive(parentID, target, visited) {
			return true
		}
	}

	return false
}

// compareUUIDs compares two UUIDs lexicographically.
func compareUUIDs(a, b UUID) int {
	for i := 0; i < 16; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

// Conflict represents a conflict between events.
type Conflict struct {
	// Key is the key with the conflict.
	Key string
	// Events are the conflicting events.
	Events []*Event
	// Winner is the winning event.
	Winner *Event
	// Resolution is the resolution method.
	Resolution string
}

// NewConflict creates a new conflict.
func NewConflict(key string, events []*Event, winner *Event, resolution string) *Conflict {
	return &Conflict{
		Key:        key,
		Events:     events,
		Winner:     winner,
		Resolution: resolution,
	}
}

// MergeResult represents the result of a merge operation.
type MergeResult struct {
	// Events are the sorted events.
	Events []*Event
	// State is the resulting state.
	State map[string]*Event
	// Conflicts are the conflicts that were resolved.
	Conflicts []*Conflict
}

// NewMergeResult creates a new merge result.
func NewMergeResult(events []*Event, state map[string]*Event, conflicts []*Conflict) *MergeResult {
	return &MergeResult{
		Events:    events,
		State:     state,
		Conflicts: conflicts,
	}
}

// Merger performs merge operations on event graphs.
type Merger struct {
	graph *EventGraph
}

// NewMerger creates a new merger.
func NewMerger(graph *EventGraph) *Merger {
	return &Merger{
		graph: graph,
	}
}

// Merge merges the events in the graph and returns the resulting state.
func (m *Merger) Merge() (*MergeResult, error) {
	// Sort the events topologically
	events, err := m.graph.TopologicalSort()
	if err != nil {
		return nil, err
	}

	// Apply the events in order
	state := make(map[string]*Event)
	var conflicts []*Conflict

	for _, event := range events {
		key := event.Operation.Key()
		prevEvent, exists := state[key]

		switch event.Operation.Type() {
		case EventTypePut:
			if exists {
				// Check if this is a conflict
				if prevEvent.Operation.Type() == EventTypePut || prevEvent.Operation.Type() == EventTypeDelete {
					// Create a conflict
					conflict := NewConflict(
						key,
						[]*Event{prevEvent, event},
						event, // Last-writer-wins
						"Last-writer-wins based on Lamport clock",
					)
					conflicts = append(conflicts, conflict)
				}
			}
			// Update the state
			state[key] = event

		case EventTypeDelete:
			if exists {
				// Check if this is a conflict
				if prevEvent.Operation.Type() == EventTypePut {
					// Create a conflict
					conflict := NewConflict(
						key,
						[]*Event{prevEvent, event},
						event, // Last-writer-wins
						"Last-writer-wins based on Lamport clock",
					)
					conflicts = append(conflicts, conflict)
				}
			}
			// Update the state
			state[key] = event

		case EventTypeMerge:
			// Merge operations are handled specially
			mergeOp, ok := event.Operation.(*MergeOperation)
			if !ok {
				return nil, fmt.Errorf("invalid merge operation: %v", event.Operation)
			}

			// Get the events being merged
			var mergedEvents []*Event
			for _, eventID := range mergeOp.EventIDs() {
				mergedEvent, err := m.graph.GetEvent(eventID)
				if err != nil {
					return nil, err
				}
				mergedEvents = append(mergedEvents, mergedEvent)
			}

			// Create a conflict
			conflict := NewConflict(
				key,
				mergedEvents,
				event,
				mergeOp.Resolution(),
			)
			conflicts = append(conflicts, conflict)

			// Update the state
			state[key] = event
		}
	}

	return NewMergeResult(events, state, conflicts), nil
}

// ExplainMerge generates a human-readable explanation of the merge result.
func (m *Merger) ExplainMerge(result *MergeResult, key string) (string, error) {
	// Get the current state for the key
	event, exists := result.State[key]
	if !exists {
		return fmt.Sprintf("Key: %s\nStatus: Not found", key), nil
	}

	// Find conflicts for the key
	var keyConflicts []*Conflict
	for _, conflict := range result.Conflicts {
		if conflict.Key == key {
			keyConflicts = append(keyConflicts, conflict)
		}
	}

	// Generate the explanation
	var explanation string
	explanation += fmt.Sprintf("Key: %s\n", key)

	switch event.Operation.Type() {
	case EventTypePut:
		putOp, ok := event.Operation.(*PutOperation)
		if !ok {
			return "", fmt.Errorf("invalid put operation: %v", event.Operation)
		}
		explanation += "Status: Active"
		if len(keyConflicts) > 0 {
			explanation += " (conflicted)"
		}
		explanation += "\n"
		explanation += fmt.Sprintf("Current Value: %q (from replica %s at %s)\n",
			string(putOp.Value()), event.ReplicaID.String(), event.Timestamp.Format(time.RFC3339))

	case EventTypeDelete:
		deleteOp, ok := event.Operation.(*DeleteOperation)
		if !ok {
			return "", fmt.Errorf("invalid delete operation: %v", event.Operation)
		}
		explanation += "Status: Deleted"
		if len(keyConflicts) > 0 {
			explanation += " (conflicted)"
		}
		explanation += "\n"
		explanation += fmt.Sprintf("Reason: %q (from replica %s at %s)\n",
			deleteOp.Reason(), event.ReplicaID.String(), event.Timestamp.Format(time.RFC3339))

	case EventTypeMerge:
		mergeOp, ok := event.Operation.(*MergeOperation)
		if !ok {
			return "", fmt.Errorf("invalid merge operation: %v", event.Operation)
		}
		explanation += "Status: Merged\n"
		explanation += fmt.Sprintf("Resolution: %s (from replica %s at %s)\n",
			mergeOp.Resolution(), event.ReplicaID.String(), event.Timestamp.Format(time.RFC3339))
	}

	// Add conflict information
	if len(keyConflicts) > 0 {
		explanation += "Conflicts:\n"
		for _, conflict := range keyConflicts {
			for _, e := range conflict.Events {
				winner := ""
				if e.ID == conflict.Winner.ID {
					winner = " [WINNER]"
				}

				switch e.Operation.Type() {
				case EventTypePut:
					putOp, ok := e.Operation.(*PutOperation)
					if !ok {
						return "", fmt.Errorf("invalid put operation: %v", e.Operation)
					}
					explanation += fmt.Sprintf("  - Put %q (from replica %s at %s)%s\n",
						string(putOp.Value()), e.ReplicaID.String(), e.Timestamp.Format(time.RFC3339), winner)

				case EventTypeDelete:
					deleteOp, ok := e.Operation.(*DeleteOperation)
					if !ok {
						return "", fmt.Errorf("invalid delete operation: %v", e.Operation)
					}
					explanation += fmt.Sprintf("  - Delete %q (from replica %s at %s)%s\n",
						deleteOp.Reason(), e.ReplicaID.String(), e.Timestamp.Format(time.RFC3339), winner)
				}
			}
			explanation += fmt.Sprintf("Resolution: %s\n", conflict.Resolution)
		}
	}

	return explanation, nil
}
