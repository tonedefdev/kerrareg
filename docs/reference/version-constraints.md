# Version Constraints


Kerrareg supports all standard OpenTofu/Terraform version constraint syntax:

| Syntax | Example | Meaning |
|--------|---------|---------|
| Exact | `1.2.0` | Only version 1.2.0 |
| Comparison | `>= 1.0.0, < 2.0.0` | Any 1.x version |
| Pessimistic | `~> 1.2.0` | >= 1.2.0, < 1.3.0 (bugfixes only) |
| Pessimistic (minor) | `~> 1.2` | >= 1.2.0, < 2.0.0 |
| Exclusion | `>= 1.0.0, != 1.5.0` | Any 1.x except 1.5.0 |

