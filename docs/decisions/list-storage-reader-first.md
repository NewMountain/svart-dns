# Reader-first activation of typed list storage

Typed list rules are versioned rows in the existing list tables. Earlier binaries cannot interpret those rows. A successful startup or a health check alone therefore cannot establish safe recovery: production-list replay showed that an older reader silently lost existing filtering decisions.

Deploy a distinct reader bridge before activating the typed writer. The bridge contains the complete typed reader, record-type-aware cache and DNS evaluation, but initially refreshes lists through the preserved legacy parser. A normal active build then enables the typed writer in the startup schema transaction. The local `local_list_storage_capabilities` singleton persists that activation; it is not a synchronized user setting. A bridge started later against the same generation observes the activation and uses the typed parser, preserving new rules during subsequent refreshes.

Capability read errors abort a refresh. The publication transaction checks that parsing used the current capability, so an in-flight legacy download cannot overwrite an activated generation. Before activation, typed rows cannot be published. Inactive bridge refreshes remain unassessed by the new compatibility API.

History samples follow the same transaction's durable capability: before activation they retain the frozen reader's comma-separated format; after activation they use versioned JSON so commas inside complete rule text remain intact. The full changelog is retained independently of these display samples.

The production image selects the role with the validated `LIST_WRITER_ROLE` build argument, linked as `github.com/yeti/svart-dns/internal/svart.listWriterRole`. Ordinary Go builds default to `active`. The first transition image explicitly defaults to `bridge`; a separate follow-up source revision changes the production default to `active` after the bridge is independently qualified and sealed on every node. Do not enable the writer while any serving or automatic recovery reader lacks typed support.

The infrastructure pipeline preserves a separately frozen recovery image. Advancing it requires a reviewed manifest binding the qualified bridge's source, immutable image identity, prior recovery identity and qualification evidence. Both node stage leases remain held while their sealed bridge identities are checked and advanced. Only then may the separate active release begin. Previous baseline records and all generation evidence are retained; no old database is restored over accepted data.

Validation covers legacy parser equivalence on the production corpus, new reader equivalence on legacy rows, activated typed behavior after a bridge restart, read and transaction failures, stale refresh rejection, and the existing staged release and recovery checks. This transition adds no disk wait to DNS replies.
