# n1 – your digital Iron‑Man suit 🛡️

**n1** is a personal knowledge & security workbench that lets you collect, encrypt and query everything you want to keep safe but close‑at‑hand – notes, credentials, configs, scrap‑code, even small binaries.  Think of it as a *lock‑box‑as‑a‑library* that you can embed in any Go project **or** use from the CLI.

> **Status:** experimental • API subject to change • PRs welcome

---

## ✨  Why n1?

* **Application‑level encryption** – data are encrypted *field‑by‑field* with a master key stored in the OS secret‑store (Keychain/DPAPI/libsecret/`~/.n1‑secrets`).
* **SQLite everywhere** – zero external dependencies, works on Linux, macOS & Windows.
* **Embeddable** – import `github.com/n1/n1` in Go and drive the engine directly.
* **CLI first** – the `bosr` command (`bᴏx ‑ ᴏᴘᴇɴ ‑ sᴇᴀʟ ‑ ʀᴏᴛᴀᴛᴇ`) handles the common workflows so you don’t have to write code.
* **Cross‑platform secret store** – thin wrappers over Keychain, DPAPI and a fall‑back file store.
* **Tiny, typed core** – < 2 kLOC; easy to audit, easy to hack.

---

## 🚀  Quick start

```bash
# grab the CLI
$ go install github.com/n1/n1/cmd/bosr@latest

# 1⃣  create a new vault (generates & stores a 256‑bit key)
$ bosr init ~/vault.db
✓ Master key generated and stored for /home/you/vault.db
✓ Plaintext vault file created: /home/you/vault.db

# 2⃣  sanity‑check that everything is wired up
$ bosr open ~/vault.db
✓ Key found in secret store for /home/you/vault.db
✓ Vault check complete: database file '/home/you/vault.db' is accessible.
```

> **NOTE** – encryption of user data is application‑level and will land in the next milestone.  The DB file itself is currently plaintext.

---

## 🗺️  Project layout

| path | what lives here |
|------|-----------------|
| `cmd/bosr` | the reference CLI |
| `internal/sqlite` | DB helper that opens *unencrypted* SQLite files (SQLCipher removed – see [#13]) |
| `internal/crypto` | key generation & HKDF derivation |
| `internal/secretstore` | pluggable secret‑store (darwin/windows/linux) |
| `internal/holdr` | *future* domain model for encrypted records |
| `.devcontainer/` | VS Code / Gitpod config |

---

## 🛠️  Building from source

```bash
# clone
$ git clone https://github.com/n1/n1.git && cd n1

# run the checks
$ make test   # unit tests
$ make vet    # go vet
$ make lint   # golangci‑lint (revive, staticcheck, gosec, …)
```

The project targets **Go ≥ 1.22** (see `go.mod`).

---

## 🔬  Contributing

Found a bug? Have an idea? Open an issue or a PR!  We follow the standard Go style guide and run `golangci‑lint`.  All new code **must** come with tests.

1. Fork & clone
2. `git switch ‑c feat/my‑feature`
3. Hack, test, lint
4. Send a PR – thank you! ❤️

---

## 📜  License

[MIT](LICENSE) © 2025 Matthew Maier / Lifecycle Enterprises

---

## 🙏  Acknowledgements

* Inspired by 1Password, Bitwarden & the legendary UNIX password manager `pass`.
* Built with Go, SQLite, Zerolog and the amazing OSS community.

