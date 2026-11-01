# Passkeys on a new computer and account recovery

[العربية](passkey-access.ar.md)

A passkey does not require staying on one computer. Its availability depends on
where it was saved and what the new device/browser supports.

| Situation | How to sign in |
| --- | --- |
| New Mac, same Apple Account | A passkey saved in iCloud Keychain can sync to the new approved Mac when password/passkey syncing is enabled. A device-only credential does not gain syncing merely because the computer is a Mac. |
| New Mac, Windows or Linux computer with a compatible password manager | Sign into the same passkey provider and unlock its encrypted vault. For example, Google documents Google Password Manager support in Chrome on these platforms, with a PIN or supported device-lock recovery. |
| Passkey is on an iPhone or Android phone | In a supported browser choose the phone/another-device option, scan its QR code and approve on the phone. Bluetooth proximity is normally required for this flow. This authenticates the computer without copying the phone's private key to it. |
| An enrolled FIDO2 security key is available | Use it on the new computer with a compatible browser/connection and its PIN/user-verification ceremony. It must already be registered for this owner. |
| No available credential | Use a previously enrolled password-plus-TOTP alternative, an offline recovery code, or the restricted VPS recovery procedure below. |

Provider support varies. An iCloud passkey is not automatically an Android passkey,
and not every local credential is backed up. The application uses standard
WebAuthn and lets the browser offer supported choices; it does not implement a
custom QR login protocol or require any particular password-manager vendor.

## Our application contract

1. Allow multiple separately registered passkeys for the same owner, with labels,
   creation/last-use dates and individual revocation. Synced copies of one
   credential are one registered credential, not several independent backups.
2. Offer enrollment of an independent backup before the first production use.
   Recommended setup: a daily passkey plus a separately enrolled security key or
   another provider, and offline recovery codes stored separately. The server
   cannot prove independence from device labels or count devices in a sync account.
3. After signing into a new computer using the phone/backup, allow **Add passkey**
   with fresh strong authentication. Keep the old working credential until the
   new one has been tested. Do not constrain enrollment to platform authenticators
   in a way that excludes security keys or hybrid phone sign-in.
4. Reject removal of the last usable login method. Revoking a credential in the
   blog blocks that credential, including synced copies with the same ID, even if
   the provider retains it. Document domain/RP-ID migration before changing hosts.
5. Keep optional password + TOTP, without password-only access. Do not enable it
   silently as a new-device shortcut. Every fallback obeys throttling and CSRF rules.

## Recovery when devices are lost

Use a surviving enrolled credential first. Offline single-use recovery codes
follow the appendix: with password/TOTP they replace the second factor after
password verification; in passkey-only mode they grant only short-lived factor
re-enrollment, never publishing access. Store hashes; consume codes atomically.

If all credentials/codes are gone, an operator who still controls the VPS can run
the local recovery command. It revokes sessions/challenges and issues a one-use
restricted enrollment grant. There is no public route to disable MFA. Do not put
grants in URLs, Git, shell history or logs. Re-enroll, sign in again, and inspect
the security audit. The command is `./build/blog recover`; see the [implemented setup and recovery guide](owner-security.md).

If the passkeys, recovery codes, optional fallback **and VPS access** are all lost,
the application cannot safely prove ownership or reconstruct a private key.
Maintain independent VPS/provider recovery arrangements. A database backup
contains public credentials, not a replacement for the owner's private keys.

## Required verification

Test a new browser/device with no local passkey, phone-assisted login, a second
credential, a security key where available, cancellation/unsupported browser,
loss of one credential, consumed recovery-code replay and local recovery.
Record the OS/browser/provider combination actually tested. Simulated WebAuthn
checks supplement these tests; they do not prove real phone or hardware behavior.
Owner recovery is separate from anonymous readers' browser-scoped lesson progress.

## Primary sources

Checked 2026-10-23. Platform support comes from these sources; the application
contract and recovery policy above are project decisions.

- [Apple: passkeys across devices](https://support.apple.com/en-in/guide/iphone/iph82d6721b2/ios)
- [Google: storing and using passkeys in Chrome](https://support.google.com/chrome/answer/13168025?co=GENIE.Platform%3DDesktop&hl=en)
- [FIDO Alliance: cross-device authentication and security keys](https://fidoalliance.org/passkeys/)
- [W3C: WebAuthn](https://www.w3.org/TR/webauthn-3/)
