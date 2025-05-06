# Milestone 1 (M1) - Mirror Implementation Plan

## Overview

Milestone 1 (M1) focuses on implementing the "Mirror" capability - a seamless, encrypted, peer-to-peer synchronization mechanism across two or more replicas. This document outlines the detailed implementation plan for M1, based on the project requirements and specifications.

## 1. Goal & Success Criteria

| Item                 | Description                                                                                                                                                                                                                                                                            |
| -------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Goal**             | Seamless, encrypted, peer-to-peer sync across two or more replicas, delivering eventual consistency while preserving the append-only, content-addressed data model introduced in M0.                                                                                                   |
| **Must-pass tests**  | (1) First sync of empty → populated vault.<br>(2) Bi-directional sync with >1 conflicting updates resolved deterministically.<br>(3) 500 MB resumable transfer survives mid-stream interruption.<br>(4) Continuous "follow" mode keeps two laptops within 5 s of convergence for 24 h. |
| **Baseline metrics** | **Throughput** ≥ 80 % of raw link speed for large files; **latency** ≤ 3 RTTs for small objects; **CPU** ≤ 30 % on Apple M-series / AMD Zen3.                                                                                                                                          |
| **Exit criteria**    | CI green on the above; docs & examples merged to `main`; v0.2.0-m1 tag signed; release notes posted.                                                                                                                                                                                   |

## 2. Implementation Plan

### 2.1 Protocol Design

#### Objectives
- Design a secure, efficient protocol for vault synchronization
- Define handshake, authentication, encryption layers, transfer graph, and resume IDs
- Document the protocol in `docs/specs/mirror-protocol.md`

#### Key Components
1. **Handshake Protocol**
   - Implement Noise-based handshake (XX pattern) over TCP & QUIC
   - Support both connection types for maximum compatibility
   - Include version negotiation and capability discovery

2. **Authentication & Encryption**
   - Reuse vault AES-GCM master key for authentication
   - Implement per-object key wrapping using HKDF-SHA-256 for per-session traffic keys
   - Ensure forward secrecy for sync sessions

3. **State Synchronization**
   - Implement Merkle DAG walk using existing object hashes from M0
   - Add Bloom filter for rapid "what-you-got?" probing to minimize unnecessary transfers
   - Design efficient delta synchronization mechanism

4. **Resume Logic**
   - Create 32-byte session-ID + offset map persisted in WAL
   - Implement checkpoint mechanism for resumable transfers
   - Design recovery protocol for interrupted transfers

5. **Transport Framing**
   - Implement length-prefixed slices with optional zstd chunking
   - Design efficient binary protocol for minimal overhead
   - Support both small and large object transfers efficiently

#### Deliverables
- Complete protocol specification document (`docs/specs/mirror-protocol.md`)
- Protocol security and threat model analysis
- Reference implementation of protocol components

### 2.2 Miror Core Library

#### Objectives
- Implement a pure Go library for sync functionality
- Create a state-machine based design with pluggable transport
- Implement WAL for durability and crash recovery

#### Key Components
1. **State Machine Design**
   - Implement pure functional state-machine with pluggable transport
   - Define clear state transitions and error handling
   - Support for different transport implementations (TCP, QUIC)

2. **Write-Ahead Log (WAL)**
   - Implement WAL records: `HELLO`, `OFFER`, `ACCEPT`, `DATA`, `ACK`, `COMPLETE`
   - Ensure durability and crash recovery
   - Optimize for performance while maintaining safety

3. **Flow Control**
   - Implement automatic back-pressure & congestion window (BBR-like defaults)
   - Adapt to network conditions dynamically
   - Optimize for different network environments

4. **Public API**
   - Implement core functions:
     ```go
     type Replicator struct { ... }
     func (r *Replicator) Push(ctx, peer) error
     func (r *Replicator) Pull(ctx, peer) error
     func (r *Replicator) Follow(ctx, peer) error  // bidirectional
     ```
   - Design for extensibility and future enhancements

#### Deliverables
- Complete `internal/miror` package implementation
- Comprehensive unit tests
- API documentation and examples

### 2.3 Merge Specification

#### Objectives
- Define clear rules for merging concurrent updates
- Maintain the append-only nature of the data model
- Implement deterministic conflict resolution

#### Key Components
1. **Merge Rules**
   - Maintain absolute append-only rule
   - Implement last-writer-wins on lamport-clock for primary key clashes
   - Keep tombstones for conflict history
   - Handle union of distinct objects from both sides

2. **Audit Trail**
   - Implement `bosr merge --explain` for human-readable audit trail
   - Track and log all merge decisions
   - Provide detailed conflict resolution information

#### Deliverables
- Complete merge specification document (`docs/specs/merge.md`)
- Reference implementation in `internal/merge`
- Comprehensive test suite for merge scenarios

### 2.4 Sync Worker Implementation

#### Objectives
- Implement a daemon process for background synchronization
- Support both one-time and continuous sync modes
- Ensure efficient resource usage

#### Key Components
1. **Daemon Implementation**
   - Create `cmd/mirord` daemon with systemd + launchd units
   - Implement proper lifecycle management
   - Support for automatic startup and graceful shutdown

2. **Peer Discovery**
   - Implement mDNS-based peer discovery
   - Support manual peer configuration via `bosr peer add`
   - Handle network changes and reconnection

3. **Sync Management**
   - Implement efficient scheduling of sync operations
   - Support for prioritization of sync tasks
   - Monitor and report sync status

#### Deliverables
- Complete `cmd/mirord` implementation
- System service definitions (systemd, launchd)
- Documentation for setup and configuration

### 2.5 CLI User Experience

#### Objectives
- Enhance the CLI with sync-related commands
- Provide intuitive and informative user interface
- Support both one-time and continuous sync modes

#### Key Components
1. **Command Implementation**
   - Add `bosr sync peer-alias` for one-time sync
   - Implement `bosr sync --follow` for continuous sync
   - Add global configuration flags for sync behavior

2. **Progress UI**
   - Design and implement progress indicators for sync operations
   - Show transfer rates, estimated time, and completion percentage
   - Provide clear status information

3. **Configuration**
   - Add sync-related configuration options
   - Support for peer management
   - Implement sensible defaults with override options

#### Deliverables
- Enhanced CLI with sync commands
- User documentation for sync features
- Example usage scenarios

### 2.6 Test Harness & Fixtures

#### Objectives
- Create comprehensive test infrastructure for sync functionality
- Simulate various network conditions and failure scenarios
- Ensure robustness and reliability

#### Key Components
1. **Test Environment**
   - Implement docker-compose "mini-internet" for network simulation
   - Create chaos monkey for random failures and network issues
   - Generate 5 GB random corpus for performance testing

2. **Test Scenarios**
   - Implement tests for all must-pass criteria
   - Add tests for edge cases and failure scenarios
   - Create performance benchmarks

3. **CI Integration**
   - Integrate tests with GitHub Actions
   - Implement matrix testing across platforms (macOS, Linux, Windows)
   - Set up automated reporting of test results

#### Deliverables
- Complete test harness implementation
- Comprehensive test suite
- CI configuration for automated testing

### 2.7 Documentation & Examples

#### Objectives
- Create clear, comprehensive documentation for sync features
- Provide examples for common use cases
- Include architecture diagrams and security information

#### Key Components
1. **User Documentation**
   - Write tutorial for sync setup and usage
   - Document configuration options and best practices
   - Create troubleshooting guide

2. **Technical Documentation**
   - Create architecture diagrams
   - Document protocol details
   - Add threat-model appendix

3. **Examples**
   - Provide example scripts for common scenarios
   - Include sample configurations
   - Add demo setups

#### Deliverables
- Complete user and technical documentation
- Architecture diagrams
- Example configurations and scripts

### 2.8 Release & QA

#### Objectives
- Ensure high quality of the final release
- Complete all exit criteria
- Prepare for public release

#### Key Components
1. **Quality Assurance**
   - Perform comprehensive testing across platforms
   - Validate all must-pass criteria
   - Conduct security review

2. **Release Preparation**
   - Create release checklist
   - Generate changelog
   - Prepare release notes

3. **Release Process**
   - Create signed tag (v0.2.0-m1)
   - Update Homebrew formula
   - Publish release

#### Deliverables
- Completed release checklist
- Signed tag and release notes
- Updated Homebrew formula

## 3. Timeline & Milestones

| Week  | Checkpoint           | Deliverable                                                 |
| ----- | -------------------- | ----------------------------------------------------------- |
| **2** | Protocol-spec freeze | Reviewed spec PR, threat-model signed off.                  |
| **4** | Alpha sync           | CLI one-shot push/pull succeeds in LAN.                     |
| **6** | Beta                 | Mirord in follow-mode, basic merge passes tests, docs 50 %. |
| **8** | Release candidate    | All exit criteria green in CI; public tag cut.              |

## 4. Risks & Mitigations

| Risk                                        | Likelihood | Impact | Mitigation                                                 |
| ------------------------------------------- | ---------- | ------ | ---------------------------------------------------------- |
| QUIC implementation quirks on older routers | Med        | Med    | Fallback to TCP; env var to force.                         |
| WAL corruption on abrupt power loss         | Low        | High   | fsync every N KB; recovery tool.                           |
| Merge rule edge-cases unanticipated         | Med        | Med    | Early property-based fuzz tests; run against seed corpora. |
| Scope creep (e.g. gateway relay)            | Med        | Med    | Defer to M2 "Mesh" milestone.                              |

## 5. Implementation Progress Tracking

| Component                | Status      | Assigned To | Notes                                      |
|--------------------------|-------------|-------------|-------------------------------------------|
| Protocol Design          | Not Started | -           | Pending initial design discussions         |
| Miror Core Library       | Not Started | -           | Depends on protocol design                 |
| Merge Specification      | Not Started | -           | Requires consensus on merge semantics      |
| Sync Worker              | Not Started | -           | Depends on core library implementation     |
| CLI UX                   | Not Started | -           | Depends on core library implementation     |
| Test Harness & Fixtures  | Not Started | -           | Can be started in parallel with design     |
| Documentation & Examples | Not Started | -           | Ongoing throughout development             |
| Release & QA             | Not Started | -           | Final phase                                |

## 6. Next Steps

1. Begin protocol design discussions and draft initial protocol specification
2. Set up test harness infrastructure for early testing
3. Start implementation of core library components
4. Regular progress reviews and adjustments to the plan as needed

This plan will be updated regularly as implementation progresses.