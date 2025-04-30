# n1 Design Decisions & Conventions

This document records significant architectural decisions made during the development of n1 and outlines the conventions contributors should follow.

## Design Decisions (Architecture Decision Records - ADRs)

We use Architecture Decision Records (ADRs) to document important architectural choices, the context surrounding them, the alternatives considered, and the consequences of the chosen approach. This helps maintain consistency and provides valuable context for future development.

ADRs are written when decisions have a significant impact on the system's architecture, non-functional characteristics (like security or performance), dependencies, or development practices.

### ADR Format

New ADRs should follow a simple template (similar to [template.md](6_DESIGN_DECISIONS/template.md) in the planned expanded structure), typically including:

*   **Status:** (Proposed, Accepted, Deprecated, Superseded)
*   **Context:** What problem or situation prompted this decision?
*   **Decision:** What is the change being proposed or implemented?
*   **Consequences:** What are the results of making this decision (positive and negative)?
*   **Alternatives Considered:** What other options were evaluated?

### Accepted Decisions

*   **ADR-001: Application-Level Encryption Strategy**
    *   **Status:** Accepted
    *   **Context:** The need to encrypt user data within the vault securely and robustly, while supporting key rotation and minimizing external dependencies. Alternatives included using SQLCipher for full database encryption.
    *   **Decision:** Implement encryption at the application layer using AES-256-GCM for individual record values (`value` blob in the `vault` table). The master key is stored separately via `internal/secretstore`. The SQLite database file itself remains unencrypted.
    *   **Consequences:**
        *   (+) Simplifies build process (no CGO/SQLCipher dependency needed for core storage).
        *   (+) Allows granular encryption (potentially different keys per blob type in the future).
        *   (+) Key rotation requires re-encrypting data row-by-row within the application.
        *   (+) Metadata (`key`, timestamps) remains unencrypted in the database file.
        *   (-) Requires careful implementation in the DAO layer to ensure all data is encrypted/decrypted correctly.

*   **ADR-002: Atomic Key Rotation Strategy**
    *   **Status:** Accepted
    *   **Context:** The `bosr key rotate` command must be resilient to interruption or failure to prevent data loss or vault corruption, especially as vaults grow large. Simple in-place re-encryption or basic temp file swaps have failure modes.
    *   **Decision:** Implement key rotation using a Backup + Temporary File strategy:
        1.  Create a backup (`.bak`) of the original vault.
        2.  Create and initialize a new temporary vault file (`.tmp`).
        3.  Read/decrypt records from the original vault (using the old key) and write/encrypt them into the temporary vault (using the new key). Provide progress updates.
        4.  If successful, update the key in the secret store.
        5.  Atomically rename the temporary file to replace the original.
        6.  Delete the backup file.
        7.  Crucially, on *any* failure after the backup is created, leave the backup file intact for manual recovery and provide clear error messages about the state. Pre-flight checks for disk space are included.
    *   **Consequences:**
        *   (+) Significantly higher data safety during rotation compared to simpler methods. Clear recovery path (`.bak`) on failure.
        *   (+) Handles interruptions more gracefully.
        *   (-) Requires temporarily up to 3x the vault size in disk space during rotation.
        *   (-) Rotation time is proportional to vault size (backup + full data rewrite).
        *   (-) Requires careful implementation of cleanup logic, especially on error paths.

*(Future ADRs will be added here as needed)*

---

## Development Conventions

Following these conventions ensures code consistency, maintainability, and ease of collaboration.

### Coding Style

*   **Go Standards:** All Go code **must** be formatted with `gofmt` or `goimports`.
*   **Linting:** Code should pass checks configured in `.golangci.yml` using `golangci-lint`. Address reported issues before submitting PRs.
*   **Simplicity:** Prefer clear, simple code over overly clever or complex solutions. Follow standard Go idioms.
*   **Error Handling:** Use `fmt.Errorf` with the `%w` verb to wrap errors where appropriate, preserving context. Check errors explicitly; avoid discarding them with `_` unless absolutely necessary and justified.
*   **Logging:** Use the shared `internal/log` package (based on `zerolog`) for structured logging. Use appropriate levels (Debug, Info, Warn, Error, Fatal). Include relevant context fields.
*   **Comments:** Write comments to explain *why* code does something, especially for non-obvious logic, rather than just *what* it does.

### Testing

*   **Importance:** Comprehensive tests are critical for ensuring correctness and robustness.
*   **Unit Tests:** Place unit tests alongside the code they test (e.g., `foo_test.go` next to `foo.go`) within the `internal/` packages. Aim for good coverage of functions and edge cases.
*   **Integration Tests:** Place CLI and cross-component tests in the top-level `test/` directory. These tests often execute the compiled `bosr` binary.
*   **Framework:** Use the standard Go `testing` package and assertions/helpers from `github.com/stretchr/testify` (primarily `require` for fatal test errors and `assert` for non-fatal checks).
*   **Table-Driven Tests:** Use table-driven tests where appropriate to cover multiple input/output variations concisely.
*   **CI:** All tests are run automatically via GitHub Actions on pushes and PRs. Tests must pass for a PR to be merged.

### Commit Messages

*   **Format:** Follow the [Conventional Commits](https://www.conventionalcommits.org/) specification. This helps automate changelog generation and makes commit history easier to understand.
    *   Examples:
        *   `feat: add --dry-run flag to key rotate command`
        *   `fix: prevent panic when opening empty vault file`
        *   `docs: update roadmap with M1 details`
        *   `refactor: improve error handling in secretstore`
        *   `test: add integration test for canary check failure`
        *   `chore: update golangci-lint version`
*   **Scope:** Use scopes optionally for clarity (e.g., `feat(cli): ...`, `fix(crypto): ...`).
*   **Body/Footer:** Use the commit body to provide more context if the title isn't sufficient. Use the footer to reference related issues (e.g., `Fixes #123`).

### Branching Strategy

*   **`main`:** Represents the latest stable, released (or pre-release milestone) state. Protected branch. Direct pushes disallowed.
*   **Feature Branches:** All new work (features, fixes, refactors) should be done on branches named descriptively, typically prefixed (e.g., `feat/add-sync-command`, `fix/key-rotation-cleanup`). Create branches off `main`.
*   **Pull Requests (PRs):** Submit PRs from your feature branch targeting `main`.

### Pull Requests (PRs)

*   **Focus:** Keep PRs small and focused on a single logical change or feature.
*   **Description:** Provide a clear description of the changes, the motivation, and how to test (if applicable). Link to any relevant issues.
*   **CI:** Ensure all CI checks (build, lint, test) pass before marking a PR as ready for review.
*   **Review:** Engage constructively in code reviews. Be open to feedback and provide clear explanations for your implementation choices.

### Dependency Management

*   **Go Modules:** Use Go Modules (`go.mod`, `go.sum`) for managing dependencies.
*   **Minimization:** Strive to keep the number of external dependencies low. Prefer standard library solutions where practical.
*   **Vetting:** Carefully evaluate any new third-party dependencies for security, maintenance status, and necessity before adding them.

---

This document is a living guide. Conventions may evolve; changes should be discussed and documented here.