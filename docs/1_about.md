# About n1

This document outlines the core purpose, guiding principles, and key terminology of the n1 project.

## Mission & Vision

**n1** is a personal knowledge & security workbench designed to help you collect, encrypt, and query everything you want to keep safe but close at hand – notes, credentials, configs, scrap‑code, even small binaries.

Our mission is two-fold:

1.  **Replace extraction with augmentation.**
    Today’s platforms often monetize your attention and data. N1 flips that: it aims to amplify *your* effectiveness so clearly that it becomes indispensable.

2.  **Turn overwhelm into an exhale.**
    Capture everything with minimal friction, trust it’s safe and private, and retrieve it instantly when needed. That feeling of calm clarity—“n1 is holding it for me”—is the product’s emotional core. We want users to walk away feeling lighter and more in control.

## Priorities & Values

The development of n1 is guided by the following core principles:

*   **Robustness & Reliability:** Protecting user data is paramount. Features, especially those involving data transformation (like key rotation), must be designed to prevent data loss, even if interrupted. Data integrity is non-negotiable.
*   **Privacy & Security:** All user data (Holds, Blobs) is encrypted at rest using strong, modern cryptography (AES-GCM). The master key is protected by the OS secret store. The default posture is local-first, minimizing external dependencies or data leakage.
*   **User Control & Ownership:** Users own their data and the keys that protect it. There are no required accounts or cloud dependencies by default. Features should empower users, not lock them in. Export and backup capabilities are essential.
*   **Augmentation & Effectiveness:** n1 should actively help users manage information and tasks, making them more effective. It should reduce cognitive load, not add to it.
*   **Simplicity & Clarity:** While powerful, the core concepts and user interface should strive for simplicity. The onboarding experience should be frictionless, providing immediate value.

## Glossary

Definitions of core terms used within the n1 project:

*   **DBOS:** Digital Being Operating System – the portable software (n1) that acts as your personal, encrypted “digital alter‑ego.”
*   **Hold:** The atomic unit of information in n1. Every thought, clip, task, or file you capture becomes a Hold—an immutable JSON record encrypted at rest. All edits or summaries are separate events that reference the original Hold, never overwrite it.
*   **Blob:** Any large binary payload (PDF, image, audio) linked from a Hold. Stored once, encrypted with its own key (or derived key), and content‑addressed by hash.
*   **Scope:** Human‑centered “zones” that govern attention and permissions. Default scopes: Inbox (unprocessed), Sandbox (in progress), Safebox (archived), Trashbox (discard). *(Note: Implementation detail for future milestones)*
*   **Event Log:** Append‑only ledger of every action (create_hold, move_scope, tag, …). It is the single source of truth for sync/replay. *(Note: Aligns with future sync/M1 design)*
*   **Suspicion Score:** Real‑time confidence metric (0–100) that the active user/device is truly you, based on hard auth (passkeys) and soft context (typing rhythm, familiar network). *(Note: Future concept)*
*   **Capability Advisor:** The dialog n1 opens when your stated goal requires new resources (e.g., larger local model or a paid API). It explains costs/benefits and provisions the chosen option. *(Note: Future concept)*
*   **UI Tiers:** ① Chat/Omnibox (fast text) ② Canvas (structured in‑app view) ③ Window (handoff to third‑party app). n1 auto‑selects the tier but users can override. *(Note: Future UI concept)*