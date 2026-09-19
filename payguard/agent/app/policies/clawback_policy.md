# Clawback Policy

A clawback is what happens when a grant already given to a user (virtual currency, a bundle,
an item) needs to be taken back because the payment behind it was reversed. The normal path is
a straightforward debit of the wallet for the amount that was originally credited.

A CLAWBACK_SHORTFALL case opens when the wallet does not have enough balance to cover the full
debit -- the user has already spent some or all of what was granted. This is expected to happen
occasionally and is not, by itself, evidence of fraud. The system must never let a clawback push
an account balance negative; a debit is only ever applied up to the available balance.

When a shortfall is detected, do not attempt to auto-resolve by any means other than a partial,
balance-capped debit recorded with a clear note. Recovering the remainder (e.g. by suspending
future grants, or an off-platform collection process) is a business decision outside the scope
of what this system automates, and always requires a human to decide the next step. The agent's
job here is to surface the shortfall amount, the account history around it, and nothing more --
it does not propose a collection mechanism.

Repeated CLAWBACK_SHORTFALL cases for the same tenant/user pair within a short window are worth
flagging together: a pattern of chargebacks or refund abuse looks very different from a single
isolated shortfall, even though each individual case might look the same in isolation.
