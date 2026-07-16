# Security policy

## Reporting a vulnerability

Please report suspected vulnerabilities privately via GitHub's
[private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)
on this repository (the "Security" tab, then "Report a vulnerability").
Please do not open public issues for security reports.

We aim to acknowledge reports within five business days.

## Scope and threat model

raptorgo implements forward error correction, not authentication or
confidentiality. By design:

- FEC repairs erasures; it does not authenticate packets. A decoder fed
  forged packets can reconstruct attacker-controlled bytes, which is why the
  public decoder requires an application-supplied verifier before delivery.
- Malformed or hostile inputs must never cause unbounded memory growth,
  panics, or non-termination. The `Limits` type bounds packet sizes,
  retained symbols, decoding attempts, and aggregate allocations; violations
  of those bounds are in scope as vulnerabilities.
- Bugs that cause the decoder to deliver bytes that were never verified, or
  to diverge from RFC 6330 in a way an attacker can exploit, are in scope.

Reports about applications that skip object verification are out of
scope; that contract is documented and deliberate.
