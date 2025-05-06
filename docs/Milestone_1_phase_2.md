Milestone 1 (M1) - Mirror Implementation Status & Next Steps

## 1. Goal & Success Criteria (Unchanged)
### Goal	
Seamless, encrypted, peer-to-peer sync across two or more replicas, delivering eventual consistency while preserving the append-only, content-addressed data model introduced in M0.

### Must-pass tests
1) First sync of empty → populated vault.
2) Bi-directional sync with >1 conflicting updates resolved deterministically.
3) 500 MB resumable transfer survives mid-stream interruption.
4) Continuous "follow" mode keeps two laptops within 5 s of convergence for 24 h.

### Baseline metrics	
* Throughput ≥ 80 % of raw link speed for large files
* latency ≤ 3 RTTs for small objects
* CPU ≤ 30 % on Apple M-series / AMD Zen3.

### Exit criteria	
* CI green on the above
* docs & examples merged to main
* v0.2.0-m1 tag signed
* release notes posted.

## 2. Implementation Plan - Current Status
| Component                | Status                                       | Notes                                                                                                                                                                                                     |
|--------------------------|----------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Protocol Design          | ✅ DONE (Specification)                      | `docs/specs/mirror-protocol.md` and `docs/specs/merge.md` are defined.                                                                                                                                   |
| Miror Core Library       | 🟡 Partially Implemented (Foundation)        | Interfaces (ObjectStore, WAL, Transport), WAL (`internal/miror/wal.go`), basic TCP Transport (`internal/miror/transport.go`), message types/encoding exist.                                               |
|                          | ✅ DONE (performPush)                        | Core `Replicator.performPush` method implemented with object comparison and transfer logic.                                                                                                               |
|                          | ✅ DONE (performPull)                        | Core `Replicator.performPull` method implemented with object comparison and transfer logic.                                                                                                               |
|                          | 🟡 **Implemented (performFollow) - FAILING** | Core `Replicator.performFollow` method implemented, but `TestSyncContinuousWithNetworkChanges` **FAILS**, indicating functional issues.                                                               |
|                          | ✅ DONE (ObjectStore)                        | `ObjectStoreAdapter` (`cmd/bosr/sync.go`, `cmd/mirord/main.go`) now uses real content hashing (SHA-256 of encrypted value blobs).                                                                        |
| Merge Specification      | ✅ DONE (Specification) <br> 🟡 Implemented (Code Structure) <br> ⌛ TODO (Integration) | Spec exists. `internal/merge` package defines structures but is not yet integrated into the sync/replication process.                                                                         |
| Sync Worker (mirord)     | 🟡 Partially Implemented (Foundation, Basic Server) | `cmd/mirord` daemon exists. Basic TCP listener and connection handler (`handleConnection`) structure is present. Handles initial OFFER. **Fails on reconnection/resume scenarios.**                    |
|                          | ⌛ TODO (Features)                           | Peer discovery (mDNS) not implemented. Robust error handling, session management, and **reconnection logic** needed.                                                                                    |
| CLI UX (bosr sync)       | ✅ Implemented (Flags, Basic Calls)          | `bosr sync` command exists with flags (`--push`, `--follow`, etc.) and calls appropriate `Replicator` methods.                                                                                                |
|                          | ⌛ TODO (Features)                           | Progress reporting UI not implemented.                                                                                                                                                                    |
| Test Harness             | ✅ Implemented (Environment) <br> 🟡 Partially Implemented (Tests) | Docker Compose environment (`test/sync`) with Toxiproxy is functional. Basic `sync_test.go` tests **PASS** (likely not testing network fully). `network_test.go` structure exists.          |
|                          | 🔴 **FAILING** (Network Tests)              | `TestSyncResumableWithNetworkInterruption` and `TestSyncContinuousWithNetworkChanges` **FAIL**, indicating issues with reconnection and follow mode over the network.                                     |
| Documentation            | 🟡 Partially Implemented (Specs)             | Protocol/Merge specs exist.                                                                                                                                                                               |
|                          | ⌛ TODO                                      | User documentation and examples for sync setup/usage needed. Technical architecture diagrams for sync.                                                                                                    |
| Release & QA             | ⌛ Not Started                               | -                                                                                                                                                                                                         |

## 3. Timeline & Milestones (Revised Outlook)
The original timeline is impacted. Focus is on achieving functional network sync.
*   Previous Checkpoint: CI / Test Environment Fixed (✅ DONE)
*   Previous Checkpoint: Basic Push/Pull Logic in Replicator (✅ DONE)
*   Previous Checkpoint: Real Hashing in ObjectStoreAdapter (✅ DONE)
*   🔜 **NEXT:** Add simpler network test (`TestSyncBasicNetwork`) for baseline validation.
*   🔜 **NEXT:** Debug and fix reconnection logic (`TestSyncResumableWithNetworkInterruption` failure).
*   🔜 **NEXT:** Debug and fix follow mode logic (`TestSyncContinuousWithNetworkChanges` failure).


## 4. Risks & Mitigations (Updated)
| Risk                                        | Likelihood | Impact | Mitigation                                                 |
|---------------------------------------------|------------|--------|------------------------------------------------------------|
| QUIC implementation quirks on older routers | Med        | Med    | Fallback to TCP; env var to force. (Currently TCP only)    |
| WAL corruption on abrupt power loss         | Low        | High   | `fsync` / `PRAGMA wal_checkpoint`; recovery tool.           |
| Merge rule edge-cases unanticipated         | Med        | Med    | Early property-based fuzz tests; run against seed corpora. |
| Scope creep (e.g. gateway relay)            | Med        | Med    | Defer to M2 "Mesh" milestone.                              |
| **Complexity of Network/Reconnection Logic**| **High**   | **High** | **Add simpler tests, focused logging, step-by-step debug.** |
| **Complexity of Follow Mode State**         | **High**   | **High** | **Fix basic sync/reconnection first, detailed logging.**   |

## 5. Immediate Next Steps (Revised Order)

1.  **Add `TestSyncBasicNetwork`:** Implement a new test in `network_test.go` that performs simple, non-interrupted push and pull operations between the two vaults via the Toxiproxy setup. This will serve as a baseline network functionality check. *(Target: Ensure basic client-server communication over proxied network works)*.
2.  **Debug Reconnection (`TestSyncResumable...`):** Investigate the EOF error during sync resume. Focus on how `mirord` handles client reconnections after the Toxiproxy link is restored. Ensure the server correctly sends the initial `OFFER` message upon reconnection. Add detailed logging in `cmd/mirord/main.go`'s `handleConnection` and the client-side receive logic. *(Target: `TestSyncResumableWithNetworkInterruption` PASSES)*.
3.  **Debug Follow Mode (`TestSyncContinuous...`):** Investigate why data isn't syncing correctly in `--follow` mode. Add detailed logging within the `performFollow` loop in `internal/miror/miror.go` to trace the pull/push cycles, state changes, and message exchanges. Verify interaction with the server (`handleConnection`). *(Target: `TestSyncContinuousWithNetworkChanges` PASSES)*.
4.  **Integrate Merge Logic:** Once basic sync, resume, and follow are working, integrate the merge specification logic (`internal/merge`) into the `Replicator` to handle conflicts correctly.
5.  **Refine Tests:** Enhance tests (`sync_test.go`, `network_test.go`) to cover merge scenarios and potentially add more chaos testing.
6.  **Complete Documentation:** Write user documentation, examples, and architecture diagrams for M1.