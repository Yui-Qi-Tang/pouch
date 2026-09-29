# Recorded repair binding fixture

This is a selected subset of `artifacts/go-repair-20260929-01` from Git checkpoint `470360b828d69cafe523b8fb6d6b960fdb1223d5`. Its 18 data files are copied byte-for-byte; `manifest.json` is a new subset inventory. It records the original full manifest hash and each selected file's hash and size. It is not the original 205-entry run manifest.

The default `TestRepairPublicationBindings` uses this subset to verify all six packages, the complete forward set, 36 recorded events, source/checkpoint identities, file hashes/modes, frame conditions, return completeness and targeted rejection cases. It does not rerun SAT, execute these recorded patches or access AHE.

The opt-in native repair test continues to check the full original 205-entry run inventory, restored separately as described in [the archive guide](../../docs/artifacts.md). No raw historical evidence is rewritten by this extraction. The historical official-test control mismatch is not erased by successful file restoration; see the [current verification scope](../../docs/STATUS.md).
