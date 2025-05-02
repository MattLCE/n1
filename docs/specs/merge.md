# Merge Specification

## 1. Introduction

This document specifies the merge semantics for n1's synchronization system. It defines how concurrent updates from multiple replicas are reconciled while preserving the append-only, content-addressed data model introduced in M0.

## 2. Merge Principles

The merge system is guided by the following core principles:

1. **Append-Only Rule**: The append-only nature of the data model is absolute. Existing data is never modified or deleted.
2. **Deterministic Resolution**: Given the same inputs, all replicas must arrive at the same merged state.
3. **Causality Preservation**: If event A caused event B, then A must be ordered before B in all replicas.
4. **Conflict Minimization**: The system should minimize the occurrence of conflicts through careful design.
5. **Transparency**: When conflicts occur, their resolution should be transparent and explainable to users.

## 3. Data Model

### 3.1 Logical Clock

Each replica maintains a Lamport clock, which is a scalar value that is:
- Incremented before each local operation
- Updated to max(local_clock, received_clock) + 1 when receiving updates from another replica

The Lamport clock provides a partial ordering of events across replicas.

### 3.2 Event Structure

Each event in the system has the following structure:

```
Event {
    id: UUID,                 // Globally unique identifier
    replica_id: UUID,         // ID of the replica that created the event
    lamport_clock: uint64,    // Logical timestamp
    parent_ids: [UUID],       // IDs of parent events (causal dependencies)
    operation: Operation,     // The actual operation (Put, Delete, etc.)
    timestamp: DateTime,      // Wall-clock time (for user display only)
}
```

### 3.3 Operations

The system supports the following operations:

1. **Put**: Add or update a key-value pair
   ```
   Put {
       key: String,
       value: Blob,
       metadata: Metadata,
   }
   ```

2. **Delete**: Mark a key as deleted (tombstone)
   ```
   Delete {
       key: String,
       reason: String,
   }
   ```

3. **Merge**: Explicit merge of concurrent events
   ```
   Merge {
       event_ids: [UUID],
       resolution: Resolution,
   }
   ```

## 4. Merge Algorithm

### 4.1 Event Graph Construction

1. Each replica maintains a directed acyclic graph (DAG) of events.
2. When receiving events from another replica, they are added to the local graph.
3. The graph preserves causal relationships through parent_ids references.

### 4.2 Topological Sorting

1. Events are sorted in topological order (if A is a parent of B, A comes before B).
2. For events with no causal relationship (concurrent events), they are ordered by:
   a. Lamport clock (lower values first)
   b. If Lamport clocks are equal, by replica_id (lexicographically)

### 4.3 Conflict Detection

A conflict occurs when two or more concurrent events operate on the same key. Specifically:

1. **Put-Put Conflict**: Two or more Put operations on the same key
2. **Put-Delete Conflict**: A Put and a Delete operation on the same key
3. **Delete-Delete Conflict**: Two or more Delete operations on the same key (not actually a conflict, but tracked for completeness)

### 4.4 Conflict Resolution

Conflicts are resolved automatically using the following rules:

1. **Put-Put Conflict**:
   - The event with the higher Lamport clock wins (last-writer-wins).
   - If Lamport clocks are equal, the event from the replica with the lexicographically higher replica_id wins.
   - All versions are preserved in the event log, but only the winning version is returned by default for queries.

2. **Put-Delete Conflict**:
   - The event with the higher Lamport clock wins.
   - If the Delete wins, the key is considered deleted but the Put event is still preserved.
   - If the Put wins, the key is considered active, but the Delete tombstone is preserved.

3. **Delete-Delete Conflict**:
   - All Delete events are preserved, but they have the same effect (the key is deleted).
   - For tracking purposes, the Delete with the higher Lamport clock is considered the "winning" Delete.

### 4.5 Merge Markers

When a conflict is resolved, a Merge event is created that:
1. References all conflicting events as parents
2. Records the resolution decision
3. Is assigned a Lamport clock higher than any of its parents

This Merge event becomes part of the event graph and is synchronized like any other event.

## 5. Synchronization Process

### 5.1 Event Exchange

During synchronization:

1. Replicas exchange their event graphs (or deltas since last sync).
2. Each replica integrates the received events into its local graph.
3. The merge algorithm is applied to resolve any conflicts.
4. The resolved state becomes the new current state of the replica.

### 5.2 Consistency Guarantees

The merge system provides the following consistency guarantees:

1. **Eventual Consistency**: If all replicas stop receiving updates and can communicate, they will eventually converge to the same state.
2. **Causal Consistency**: If event A causally precedes event B, all replicas will see A before B.
3. **Monotonicity**: A replica's view of the system never goes backward in time; it only moves forward.

## 6. User Interface

### 6.1 Conflict Visibility

By default, only the winning version of a key is shown to users. However, users can:

1. View the history of a key, including all versions and conflicts.
2. See which version is currently active and why.
3. Override the automatic conflict resolution if desired.

### 6.2 Explain Command

The `bosr merge --explain` command provides a human-readable explanation of merge decisions:

```
$ bosr merge --explain mykey

Key: mykey
Status: Active (conflicted)
Current Value: "new value" (from replica R2 at 2025-05-01 14:32:45)
Conflicts:
  - Put "original value" (from replica R1 at 2025-05-01 14:30:12)
  - Put "new value" (from replica R2 at 2025-05-01 14:32:45) [WINNER]
Resolution: Last-writer-wins based on Lamport clock (R2:45 > R1:23)
```

### 6.3 Manual Resolution

Users can manually resolve conflicts using:

```
$ bosr merge --resolve mykey --select R1
```

This creates a new Merge event that explicitly selects the specified version.

## 7. Implementation Guidelines

### 7.1 Storage Efficiency

While the merge system preserves all versions, implementations should:

1. Use efficient storage for the event graph (e.g., content-addressed storage).
2. Implement garbage collection for events that are no longer needed (e.g., after explicit user resolution).
3. Consider compaction strategies for long-running systems.

### 7.2 Performance Considerations

To ensure good performance:

1. Implement incremental synchronization to exchange only new events.
2. Use efficient data structures for the event graph and topological sorting.
3. Cache resolution results to avoid recomputing them.
4. Consider bloom filters or similar techniques to quickly determine which events need to be exchanged.

### 7.3 Conflict Minimization

To minimize conflicts:

1. Encourage users to use different keys for different data.
2. Consider implementing application-level conflict resolution for specific data types.
3. Provide real-time synchronization when possible to reduce the window for conflicts.

## 8. Edge Cases and Special Considerations

### 8.1 Clock Skew

While Lamport clocks provide a partial ordering, they can lead to unintuitive results if there is significant clock skew between replicas. Implementations should:

1. Consider using hybrid logical clocks that incorporate wall-clock time when possible.
2. Provide clear explanations when clock skew might be affecting merge decisions.

### 8.2 Network Partitions

During network partitions:

1. Replicas in different partitions may diverge.
2. When the partition heals, the merge algorithm will reconcile the divergent states.
3. Users should be notified of significant merges after partition healing.

### 8.3 Large Event Graphs

For systems with large event graphs:

1. Implement pruning strategies to remove unnecessary events.
2. Consider checkpointing the state periodically to avoid traversing the entire graph.
3. Use efficient serialization formats for event exchange.

## 9. Testing and Verification

Implementations should be tested against:

1. **Property-Based Tests**: Verify that the merge algorithm satisfies its formal properties (commutativity, associativity, idempotence).
2. **Scenario Tests**: Test specific conflict scenarios and verify the expected outcomes.
3. **Chaos Tests**: Simulate network partitions, replica failures, and other adverse conditions.
4. **Performance Tests**: Verify that the system performs well with large event graphs and high conflict rates.

## Appendix A: Example Scenarios

### A.1 Simple Last-Writer-Wins

**Initial State**: Empty vault on replicas R1 and R2

**Events**:
1. R1: Put("key1", "value1") at Lamport clock 1
2. R2: Sync with R1
3. R2: Put("key1", "value2") at Lamport clock 3
4. R1: Sync with R2

**Result**:
- Both replicas have Put("key1", "value2") as the winning event
- Both replicas preserve the history of Put("key1", "value1")

### A.2 Concurrent Updates

**Initial State**: Empty vault on replicas R1 and R2

**Events**:
1. R1: Put("key1", "value1") at Lamport clock 1
2. R2 (without syncing): Put("key1", "value2") at Lamport clock 1
3. R1 and R2 sync

**Result**:
- If R1's replica_id < R2's replica_id, then "value2" wins
- If R1's replica_id > R2's replica_id, then "value1" wins
- Both replicas preserve both versions
- A Merge event is created to record the resolution

### A.3 Delete Conflict

**Initial State**: Both replicas have key1="value1"

**Events**:
1. R1: Delete("key1") at Lamport clock 5
2. R2 (without syncing): Put("key1", "value2") at Lamport clock 6
3. R1 and R2 sync

**Result**:
- Put wins because it has a higher Lamport clock
- key1="value2" on both replicas
- Both replicas preserve the Delete tombstone
- A Merge event is created to record the resolution

## Appendix B: Formal Properties

The merge algorithm satisfies the following formal properties:

### B.1 Commutativity

For any two sets of events A and B:
```
merge(A, B) = merge(B, A)
```

### B.2 Associativity

For any three sets of events A, B, and C:
```
merge(merge(A, B), C) = merge(A, merge(B, C))
```

### B.3 Idempotence

For any set of events A:
```
merge(A, A) = A
```

### B.4 Identity

For the empty set of events ∅:
```
merge(A, ∅) = A
```

These properties ensure that the merge algorithm is well-behaved and will converge regardless of the order in which events are received.