# n1 Project Roadmap

This document outlines the planned milestones for the development of n1. Each milestone builds upon the previous one, progressively adding core capabilities.

**Status Legend:**

*   ✅ **DONE:** Milestone complete and merged to `main`.
*   🔜 **NEXT:** Currently in active development or the immediate next focus.
*   queued: Planned but not yet started.

---

## Milestones

*   ✅ **DONE (v0.1.0-m0) M0 – Trust** _(immutable vault, secrets, CLI, CI)_
    *   Vault schema definition & migration system (`migrations/`).
    *   Row-level AEAD (AES-GCM) encryption for stored data.
    *   Master-key rotation command (`bosr rotate`) with dry-run and progress feedback.
    *   Cross-platform secret store integration (`internal/secretstore`).
    *   Core CLI commands: `bosr init / open / put / get / rotate` (`cmd/bosr`).
    *   Unit testing, integration testing, and GitHub Actions CI workflow.
    *   Initial domain model structure (`internal/holdr`).

*   🔜 **NEXT M1 – Mirror**
    *   Device-to-device synchronization mechanism (encrypted push/pull).
    *   Resumable sync transfers.
    *   Eventual consistency model for replicas.
    *   CLI daemon mode for continuous sync (`bosr sync --follow`).
    *   Synchronization library (`miror` lib).
    *   Sync worker implementation.
    *   Merge specification for handling concurrent updates (considering append-only nature).
    *   Additional integration tests covering sync scenarios.

*   queued **M2 – Visor α**
    *   Local web UI server (`visor` server) for browsing/creating Holds.
    *   Frontend built with HTMX and Tailwind CSS (static assets).
    *   Mechanism to unlock the UI via the master key.

*   queued **M3 – Export / backup**
    *   Functionality to export the vault to an encrypted bundle (e.g., using `age` encryption).
    *   Functionality to import from an encrypted bundle.
    *   Baseline permission concepts (TBD).

*   queued **M4 – Multichannel · Multibox**
    *   Ability to mount and interact with multiple vaults simultaneously.
    *   Multi-device presence awareness.
    *   Push notification system (details TBD).

*   queued **M5 – Scopr**
    *   Implementation of Scope objects (Inbox, Sandbox, Safebox, etc.).
    *   Precision-profile engine (details TBD).

*   queued **M6 – Spotr**
    *   Coordinate packets concept (details TBD).
    *   Deduplication and merging logic for data.
    *   Spatial indexing capabilities (details TBD).

*   queued **M7 – Howr**
    *   Recursive action graph implementation.
    *   Weighted edges for graph analysis (details TBD).

*   queued **M8 – Integrations**
    *   Ingestion mechanisms for E-mail.
    *   File ingestion capabilities.
    *   Calendar integration (details TBD).
    *   Other third-party integrations.

---

*This roadmap is subject to change based on development progress, feedback, and evolving priorities.*