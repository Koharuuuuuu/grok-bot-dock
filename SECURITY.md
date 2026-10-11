# Security Policy

Bolt Dock is intended for self-hosting. Please do not report suspected vulnerabilities in public issues.

## Reporting a vulnerability

Contact the repository owner privately through GitHub's security advisory reporting feature, if enabled, or use the private contact details on the owner's GitHub profile. Include the affected version, impact, and safe reproduction steps. Do not include real access tokens, production reports, or personal data.

Please allow time for investigation and a fix before publicly disclosing a vulnerability. This project does not currently publish a supported-version or response-time guarantee.

## Deployment notes

- Use a long, random `ACCESS_TOKEN`; do not expose it in URLs, prompts, reports, logs, or screenshots.
- Put public deployments behind HTTPS and configure rate limits at the reverse proxy.
- The service is designed for a single trusted owner; it does not provide tenant isolation.
- OAuth is optional and remains a limited implementation. Review the OAuth threat model and acceptance notes before enabling it.
