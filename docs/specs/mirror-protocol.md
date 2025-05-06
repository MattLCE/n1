# Mirror Protocol Specification

## 1. Introduction

The Mirror Protocol is designed to enable secure, efficient, and resilient synchronization of n1 vaults across multiple devices. This document specifies the protocol's design, including handshake procedures, authentication mechanisms, encryption layers, transfer methodology, and resume capabilities.

## 2. Protocol Overview

The Mirror Protocol is built on the following key principles:

- **Security**: All communications are encrypted end-to-end using strong cryptography.
- **Efficiency**: The protocol minimizes data transfer by using content-addressed storage and efficient delta synchronization.
- **Resilience**: Transfers can be resumed after interruption without losing progress.
- **Eventual Consistency**: The protocol ensures that all replicas eventually converge to the same state.
- **Append-Only**: The protocol preserves the append-only nature of the n1 data model.

## 3. Transport Layer

### 3.1 Transport Options

The Mirror Protocol supports two transport mechanisms:

1. **QUIC** (preferred): Provides multiplexed connections over UDP with built-in encryption and congestion control.
2. **TCP** (fallback): Used when QUIC is unavailable or blocked.

Implementation must support both transport options, with automatic fallback from QUIC to TCP when necessary. An environment variable (`N1_FORCE_TCP=1`) can be used to force TCP mode.

### 3.2 Connection Establishment

1. The client attempts to establish a QUIC connection to the server.
2. If QUIC connection fails after a configurable timeout (default: 5 seconds), the client falls back to TCP.
3. Once the base transport connection is established, the protocol handshake begins.

## 4. Handshake Protocol

### 4.1 Noise Protocol Framework

The Mirror Protocol uses the Noise Protocol Framework with the XX pattern for handshake and session establishment. This provides:

- Mutual authentication
- Forward secrecy
- Identity hiding
- Resistance to man-in-the-middle attacks

### 4.2 Handshake Process

The XX pattern handshake proceeds as follows:

1. **Initiator → Responder**: `e`
   - Initiator generates an ephemeral key pair and sends the public key.

2. **Responder → Initiator**: `e, ee, s, es`
   - Responder generates an ephemeral key pair and sends the public key.
   - Both parties compute a shared secret from their ephemeral keys.
   - Responder sends its static public key (encrypted).
   - Both parties mix in a shared secret derived from initiator's ephemeral key and responder's static key.

3. **Initiator → Responder**: `s, se`
   - Initiator sends its static public key (encrypted).
   - Both parties mix in a shared secret derived from initiator's static key and responder's ephemeral key.

After the handshake, both parties have established a secure channel with the following properties:
- Mutual authentication
- Forward secrecy for all messages
- Encryption and integrity protection for all subsequent communications

### 4.3 Version Negotiation

After the Noise handshake, the protocol performs version negotiation:

1. Initiator sends a `VERSION` message containing:
   - Protocol version (current: 1)
   - Supported features as a bit field
   - Client identifier (e.g., "n1/0.2.0")

2. Responder replies with a `VERSION_ACK` message containing:
   - Selected protocol version
   - Supported features intersection
   - Server identifier

If version negotiation fails, the connection is terminated.

## 5. Authentication & Encryption

### 5.1 Key Derivation

The Mirror Protocol uses the vault's master key as the root of trust for authentication. From this master key, several derived keys are generated:

1. **Static Identity Key**: A long-term identity key derived from the master key using HKDF-SHA-256 with a fixed info string "n1-mirror-identity-v1".
2. **Per-Session Traffic Keys**: Derived from the Noise handshake and used for encrypting all session traffic.
3. **Per-Object Encryption Keys**: Derived for each object using HKDF-SHA-256 with the object's hash as the salt.

### 5.2 Encryption Algorithm

All encrypted data uses AES-256-GCM with the following properties:
- 256-bit key
- 96-bit (12-byte) nonce
- 128-bit (16-byte) authentication tag

### 5.3 Key Wrapping

For secure key exchange, the protocol uses AES-GCM key wrapping:
1. The sender encrypts the object key with the session key.
2. The wrapped key is sent along with the encrypted object.
3. The receiver unwraps the key and uses it to decrypt the object.

This approach allows for efficient re-encryption of objects when the session key changes without re-encrypting the entire object.

## 6. State Synchronization

### 6.1 Merkle DAG Walk

The primary mechanism for state synchronization is a Merkle DAG (Directed Acyclic Graph) walk:

1. Each object in the vault has a unique content-addressed hash (from M0).
2. Objects form a DAG where edges represent references between objects.
3. The sync process walks this DAG to identify differences between replicas.

The walk algorithm:
1. Start with the root objects (those with no incoming edges).
2. For each object, check if the peer has it (using its hash).
3. If not, send the object and continue with its children.
4. If yes, continue with its children.

### 6.2 Bloom Filter Optimization

To optimize the "what-you-got?" probing phase, the protocol uses Bloom filters:

1. The responder generates a Bloom filter containing hashes of all its objects.
2. The initiator queries this filter to quickly determine which objects the responder likely has.
3. Only objects that are not in the filter are considered for transfer.

Bloom filter parameters:
- Size: 10 bits per object (adaptive based on vault size)
- Hash functions: 7
- False positive rate: < 1%

### 6.3 Delta Synchronization

For efficient transfer of large objects that have changed slightly:

1. Objects are chunked using a content-defined chunking algorithm (CDC).
2. Only chunks that have changed are transferred.
3. The receiver reassembles the object from existing and new chunks.

## 7. Transfer Protocol

### 7.1 Message Types

The Mirror Protocol defines the following message types:

1. **HELLO**: Initial message to establish sync session.
2. **OFFER**: Offer of objects to transfer.
3. **ACCEPT**: Acceptance of offered objects.
4. **DATA**: Object data transfer.
5. **ACK**: Acknowledgment of received data.
6. **COMPLETE**: Indication that transfer is complete.
7. **ERROR**: Error notification.

### 7.2 Message Format

All messages follow a common format:
```
+----------------+----------------+----------------+
| Message Type   | Message Length | Message Body   |
| (1 byte)       | (4 bytes)      | (variable)     |
+----------------+----------------+----------------+
```

### 7.3 Flow Control

The protocol implements flow control to prevent overwhelming the receiver:

1. The sender maintains a congestion window similar to BBR (Bottleneck Bandwidth and RTT).
2. The receiver provides feedback on its processing capacity.
3. The sender adjusts its sending rate based on this feedback.

Initial parameters:
- Initial window: 16 KB
- Maximum window: 16 MB
- Minimum window: 4 KB

### 7.4 Transport Framing

Data is framed for efficient transport:

1. All messages are length-prefixed for easy parsing.
2. Large objects are split into chunks of configurable size (default: 64 KB).
3. Optional zstd compression is applied to chunks when beneficial.

## 8. Resume Logic

### 8.1 Session Identification

Each sync session is identified by a unique 32-byte Session ID generated using a cryptographically secure random number generator. This ID is used to associate interrupted transfers with their resumption.

### 8.2 Write-Ahead Log (WAL)

The protocol uses a Write-Ahead Log (WAL) to track transfer progress:

1. Before sending/receiving an object, a WAL entry is created.
2. The WAL entry contains:
   - Session ID
   - Object hash
   - Transfer direction (send/receive)
   - Offset map (for partial transfers)
   - Timestamp

3. WAL entries are persisted to disk and fsync'd every N KB (configurable, default: 1 MB).

### 8.3 Resume Process

When resuming an interrupted transfer:

1. The initiator sends a `HELLO` message with the previous Session ID.
2. The responder looks up the Session ID in its WAL.
3. If found, the responder sends a `RESUME` message with the last acknowledged offset.
4. The transfer continues from that offset.
5. If not found, a new session is started.

### 8.4 Cleanup

WAL entries are cleaned up:
- On successful completion of a transfer
- After a configurable expiration period (default: 7 days)
- When explicitly requested by the user

## 9. Error Handling

### 9.1 Error Types

The protocol defines the following error types:

1. **PROTOCOL_ERROR**: Invalid message format or sequence.
2. **AUTHENTICATION_ERROR**: Failed authentication.
3. **ENCRYPTION_ERROR**: Failed encryption/decryption.
4. **TRANSFER_ERROR**: Failed data transfer.
5. **RESOURCE_ERROR**: Insufficient resources (disk space, memory).
6. **TIMEOUT_ERROR**: Operation timed out.

### 9.2 Error Recovery

Error recovery depends on the error type:

1. **Transient errors** (e.g., timeouts, temporary resource issues):
   - Retry with exponential backoff.
   - Maximum retry count: 5 (configurable)

2. **Permanent errors** (e.g., authentication failures, protocol errors):
   - Terminate the session.
   - Log detailed error information.
   - Notify the user.

## 10. Security Considerations

### 10.1 Threat Model

The Mirror Protocol is designed to be secure against the following threats:

1. **Passive eavesdropping**: All communications are encrypted.
2. **Active man-in-the-middle attacks**: Prevented by mutual authentication.
3. **Replay attacks**: Prevented by using nonces and sequence numbers.
4. **Denial of service**: Mitigated by resource limits and rate limiting.

### 10.2 Known Limitations

1. The protocol does not hide metadata such as transfer timing and size.
2. The protocol assumes that the master key is kept secure.
3. The protocol does not provide protection against compromised endpoints.

### 10.3 Recommendations

1. Use the latest version of the protocol.
2. Keep the master key secure.
3. Verify peer identities before syncing.
4. Use secure networks when possible.

## 11. Implementation Guidelines

### 11.1 Minimum Requirements

Implementations must:
1. Support both QUIC and TCP transports.
2. Implement the Noise XX handshake correctly.
3. Use AES-256-GCM for encryption.
4. Implement the WAL for resumable transfers.
5. Handle all error conditions gracefully.

### 11.2 Optional Features

Implementations may:
1. Support additional transport mechanisms.
2. Implement advanced congestion control algorithms.
3. Add telemetry and monitoring capabilities.
4. Optimize for specific environments.

### 11.3 Testing

Implementations should be tested against:
1. The reference implementation.
2. Various network conditions (high latency, packet loss, etc.).
3. Interruption scenarios.
4. Resource-constrained environments.

## 12. Future Considerations

The following features are being considered for future versions of the protocol:

1. **Relay support**: Allow syncing through intermediary nodes.
2. **Partial sync**: Sync only specific subsets of the vault.
3. **Bandwidth limiting**: User-configurable bandwidth limits.
4. **Multi-path transfer**: Use multiple network paths simultaneously.
5. **Enhanced privacy**: Additional measures to hide metadata.

## Appendix A: Message Specifications

### A.1 HELLO Message
```
+----------------+----------------+----------------+----------------+
| Type (0x01)    | Length         | Session ID     | Capabilities   |
| (1 byte)       | (4 bytes)      | (32 bytes)     | (4 bytes)      |
+----------------+----------------+----------------+----------------+
```

### A.2 OFFER Message
```
+----------------+----------------+----------------+----------------+
| Type (0x02)    | Length         | Object Count   | Object Hashes  |
| (1 byte)       | (4 bytes)      | (4 bytes)      | (variable)     |
+----------------+----------------+----------------+----------------+
```

### A.3 ACCEPT Message
```
+----------------+----------------+----------------+----------------+
| Type (0x03)    | Length         | Object Count   | Object Hashes  |
| (1 byte)       | (4 bytes)      | (4 bytes)      | (variable)     |
+----------------+----------------+----------------+----------------+
```

### A.4 DATA Message
```
+----------------+----------------+----------------+----------------+----------------+
| Type (0x04)    | Length         | Object Hash    | Offset         | Data           |
| (1 byte)       | (4 bytes)      | (32 bytes)     | (8 bytes)      | (variable)     |
+----------------+----------------+----------------+----------------+----------------+
```

### A.5 ACK Message
```
+----------------+----------------+----------------+----------------+
| Type (0x05)    | Length         | Object Hash    | Offset         |
| (1 byte)       | (4 bytes)      | (32 bytes)     | (8 bytes)      |
+----------------+----------------+----------------+----------------+
```

### A.6 COMPLETE Message
```
+----------------+----------------+----------------+
| Type (0x06)    | Length         | Session ID     |
| (1 byte)       | (4 bytes)      | (32 bytes)     |
+----------------+----------------+----------------+
```

### A.7 ERROR Message
```
+----------------+----------------+----------------+----------------+
| Type (0x07)    | Length         | Error Code     | Error Message  |
| (1 byte)       | (4 bytes)      | (2 bytes)      | (variable)     |
+----------------+----------------+----------------+----------------+
```

## Appendix B: State Transition Diagram

```
                    +--------+
                    | CLOSED |
                    +--------+
                        |
                        | Connect
                        v
                    +--------+
                    | HELLO  |
                    +--------+
                        |
                        | Exchange Version
                        v
                +----------------+
                | VERSION_NEGOT. |
                +----------------+
                        |
                        | Negotiate Features
                        v
                    +--------+
                    | READY  |<---------+
                    +--------+          |
                        |               |
                        | Send OFFER    |
                        v               |
                    +--------+          |
                    | OFFER  |          |
                    +--------+          |
                        |               |
                        | Receive ACCEPT|
                        v               |
                    +--------+          |
                    | TRANSFER|          |
                    +--------+          |
                        |               |
                        | All Data Sent |
                        v               |
                    +--------+          |
                    |COMPLETE|----------+
                    +--------+
                        |
                        | Close Session
                        v
                    +--------+
                    | CLOSED |
                    +--------+
```

## Appendix C: Glossary

- **DAG**: Directed Acyclic Graph
- **CDC**: Content-Defined Chunking
- **WAL**: Write-Ahead Log
- **HKDF**: HMAC-based Key Derivation Function
- **AES-GCM**: Advanced Encryption Standard in Galois/Counter Mode
- **QUIC**: Quick UDP Internet Connections
- **RTT**: Round-Trip Time
- **BBR**: Bottleneck Bandwidth and RTT