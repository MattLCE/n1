# n1 Project Documentation

Welcome to the central documentation hub for the **n1** project – your digital Iron‑Man suit 🛡️.

This `/docs` directory serves as the **source of truth** for the project's mission, design, architecture, decisions, and development practices. It's intended for contributors (human and AI) and anyone interested in the technical underpinnings of n1.

Think of n1 as a personal knowledge & security workbench that lets you collect, encrypt, and query everything you want to keep safe but close at hand.

## Documentation Sections

Please refer to the following files for detailed information:

*   **[1_ABOUT.md](1_ABOUT.md)**
    *   **Mission & Vision:** Why n1 exists and the future we're building.
    *   **Priorities & Values:** The core principles guiding development (e.g., Robustness, Privacy, User Control).
    *   **Glossary:** Definitions of key terms used throughout the project (Hold, Scope, Blob, etc.).

*   **[2_ROADMAP.md](2_ROADMAP.md)**
    *   The current project roadmap, outlining milestones (M0, M1, etc.), their goals, status, and key artifacts.

*   **[3_SYSTEM_DESIGN.md](3_SYSTEM_DESIGN.md)**
    *   **Architecture:** High-level components, data models (Holds, append-only log), encryption strategy (master key, AEAD), storage layer (SQLite, migrations).
    *   **Features:** Details on user-facing capabilities, starting with the `bosr` CLI and planning for future features like Sync (M1).

*   **[4_DECISIONS_CONVENTIONS.md](4_DECISIONS_CONVENTIONS.md)**
    *   **Design Decisions (ADRs):** Records of significant architectural choices and the reasoning behind them (e.g., Why application-level encryption? Why the key rotation strategy?).
    *   **Development Conventions:** Guidelines for coding style, testing procedures, commit messages, branching strategy, etc.

*   **[5_CONTRIBUTING.md](5_CONTRIBUTING.md)**
    *   Practical instructions for setting up the development environment, running tests, and submitting pull requests.

---

This documentation is intended to be a living resource. Please help keep it accurate and up-to-date as the project evolves.