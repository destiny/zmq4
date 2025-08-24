# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| v25.x   | ✅ Yes             |
| < v25   | ❌ No              |

## Security Features

ZMQ4 implements comprehensive security mechanisms for production use.

### CURVE Security (RFC 25)

**Elliptic Curve Cryptography Implementation**
- **Algorithm**: Curve25519 elliptic curve
- **Key Exchange**: Elliptic Curve Diffie-Hellman (ECDH)
- **Encryption**: XSalsa20 stream cipher
- **Authentication**: Poly1305 message authenticator

**Security Properties:**
- **Perfect Forward Secrecy**: Each session uses unique ephemeral keys
- **Identity Verification**: Public key authentication prevents impersonation
- **Anti-Replay Protection**: Nonce-based message ordering prevents replay attacks
- **Man-in-the-Middle Protection**: Cryptographic verification of peer identity

### PLAIN Authentication (RFC 27)

**Username/Password Authentication**
- Simple credential-based authentication
- Suitable for trusted network environments
- Not recommended for production without additional transport security

### NULL Security

**No Authentication Mechanism**
- No encryption or authentication
- Suitable only for development and testing
- Should never be used in production environments

## Security Best Practices

### Key Management

```go
// Generate secure key pairs
serverPublic, serverPrivate, err := curve.GenerateKeypair()
if err != nil {
    log.Fatal("Failed to generate server keys:", err)
}

// Store keys securely - never hardcode in source
// Use environment variables or secure key stores
```

### Secure Configuration

```go
// Always validate security configuration
if config.Security == nil {
    return errors.New("security configuration required for production")
}

// Use appropriate security mechanism
switch env := os.Getenv("ENV"); env {
case "production":
    // Require CURVE security
    security = curve.NewSecurity(serverPublic, clientPublic, clientPrivate)
case "development":
    // Allow NULL security with warning
    log.Warning("Using NULL security - not suitable for production")
    security = null.NewSecurity()
}
```

### Network Security

- **Transport Layer**: Use TLS for additional transport security
- **Network Isolation**: Deploy in secured network segments
- **Firewall Rules**: Restrict access to messaging ports
- **Access Control**: Implement application-level authorization

## Vulnerability Reporting

We take security vulnerabilities seriously. Please follow responsible disclosure practices.

### Reporting Process

1. **Do NOT create public GitHub issues for security vulnerabilities**
2. **Email security reports to**: [security@example.com] (replace with actual email)
3. **Include the following information**:
   - Description of the vulnerability
   - Steps to reproduce the issue
   - Potential impact assessment
   - Affected versions
   - Suggested mitigation (if any)

### What to Expect

- **Acknowledgment**: Within 48 hours
- **Assessment**: Initial assessment within 7 days
- **Updates**: Regular updates on investigation progress
- **Resolution**: Security fix and disclosure timeline

## Security Hardening

### Code Security

```go
// Input validation
func validateMessage(msg *Message) error {
    if len(msg.Frames) == 0 {
        return errors.New("empty message not allowed")
    }
    if len(msg.Frames[0]) > maxFrameSize {
        return errors.New("frame size exceeds limit")
    }
    return nil
}

// Resource limits
socket.SetOption(zmq4.OptionSndHWM, 1000)  // Send high water mark
socket.SetOption(zmq4.OptionRcvHWM, 1000)  // Receive high water mark
socket.SetOption(zmq4.OptionLinger, time.Second) // Prevent resource leaks
```

### Deployment Security

- **Process Isolation**: Run messaging processes with minimal privileges
- **Container Security**: Use security contexts in container deployments
- **Monitoring**: Implement security monitoring and alerting
- **Logging**: Log security events (without exposing sensitive data)

## Cryptographic Implementation

### CURVE Security Details

**Key Exchange Process:**
1. Client generates ephemeral key pair
2. Server generates ephemeral key pair  
3. Both compute shared secret using ECDH
4. Derive encryption keys from shared secret
5. Establish secure communication channel

**Message Protection:**
- Each message encrypted with unique nonce
- Message authentication prevents tampering
- Forward secrecy through ephemeral keys

### Security Auditing

- **Code Review**: All security-related code undergoes peer review
- **Static Analysis**: Automated security scanning
- **Dependency Scanning**: Regular dependency vulnerability checks
- **Penetration Testing**: Periodic security assessments

## Compliance

### Standards Compliance
- **RFC 25**: CURVE security mechanism
- **RFC 27**: PLAIN authentication mechanism  
- **RFC 23**: ØMQ protocol specification
- **FIPS 140-2**: Cryptographic module requirements (where applicable)

### Industry Standards
- Follow OWASP secure coding practices
- Implement defense-in-depth security
- Regular security updates and patches
- Vulnerability disclosure coordination

## Security Updates

### Update Process
1. Security vulnerabilities assessed by security team
2. Patches developed and tested
3. Security advisory published
4. Updated versions released
5. Users notified through security channels

### Notification Channels
- GitHub Security Advisories
- Release notes with security highlights
- Mailing list notifications (if available)
- Community announcements

## Disclaimers

- **Cryptographic Export**: This software may be subject to export controls
- **Use at Own Risk**: Security implementations provided as-is
- **Professional Review**: Consider professional security review for critical applications
- **Compliance**: Users responsible for compliance with applicable regulations

## Contact

For security-related questions or concerns:
- **Security Email**: [security@example.com] (replace with actual contact)
- **General Issues**: GitHub Issues (for non-security bugs)
- **Documentation**: See README.md for general information

---

**Remember**: Security is a shared responsibility between the library maintainers and the users implementing it.