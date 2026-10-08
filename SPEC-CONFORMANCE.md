# Clio conformance matrix

This file maps the normative areas of [`SPEC.md`](SPEC.md) to the automated tests
that exercise them, plus the verification commands. It exists so that a spec
change can be checked against concrete coverage rather than a prose checklist.

Tests are Go tests in the repository root unless noted. Tests exercise behaviour
through HTTP and use temporary SQLite databases and content directories.

| Spec area | Representative tests |
| --- | --- |
| §8–§14 data model, tables, fields, records | `TestMetadataAndRecordCRUDValidation`, `TestTableSchemaUpdateProtectsStoredData` |
| §13.1 field names and reserved fields | `TestFieldNameRules` |
| §13.2/§13.4 references and uniqueness | `TestReferencePreventsDeletingReferencedRecord`, `TestAddingUniqueConstraintWithDuplicatesRollsBackSchemaUpdate`, `TestUniqueSemantics`, `TestReferenceRules` |
| §13.3 validation constraints | `TestValidationConstraints` |
| §14 record representation, identifiers and coercions | `TestRecordIdentifierRules`, `TestPatchSystemFieldRejected`, `TestDecimalCanonicalization`, `TestValueCoercionRejections`, `TestDecimalFormatNumericOutput` |
| §18/§19 table creation and schema updates | `TestExampleTableMetadataAndRecords`, `TestMetadataAndRecordCRUDValidation`, `TestTableSchemaUpdateProtectsStoredData`, `TestPatchTableFieldsMergesByName` |
| §22–§28 paging, sorting, filtering, distinct, grouping, aggregation | `TestRecordQueriesFilteringSortingPagingDistinctAndAggregates`, `TestQueryPageKeepsCorrectTopRowsAndOrdering`, `TestFilterOperatorSemantics`, `TestSortingRules`, `TestGroupingContract`, `TestAggregationWithFilterAndTypes` |
| §23 defaults, limits and deterministic ordering | `TestQueryDefaultsAndLimits`, `TestDeterministicDefaultOrdering`, `TestTemporalDefaultOrdering`, `TestSQLQueryPagesBoundApplicationAllocations`, `records_benchmark_test.go` |
| §29.2 time-range and bucket preconditions | `TestTimeRangePreconditions`, `TestBucketDefaultLimitAndAggregateRequirement` |
| §14.1–§14.3 exact numeric values | `TestSQLQueriesPreserveExactNumericSemantics`, `TestQuerySQLCollationsPreserveTypedOrdering` |
| §12/§29/§29.1–§29.3 time-series ranges and buckets | `TestTimeseriesNormalizesUTCAndBucketsWeeks`, `TestTimeseriesTableUITimeRangeAndBuckets` |
| §32–§34 URL namespaces, table UI and forms | `TestTableUIRootGroupEditDeleteAndErrors`, `TestTableUIGroupingAggregationFilteringAndPaging`, `TestGenericTableFormsAndOperationalPages` |
| §36–§41 content tree, directories, pages | `TestDirectoryAPICreationStillWorks`, `TestDirectoryAPIListAndDelete`, `TestPageAPIRoundTripAndPathSafety`, `TestPageAPIUpdateDeleteAndValidation`, `TestContentPathValidation`, `TestContentResourceIdentity`, `TestDirectoryChildrenAndContentUI` |
| §38/§38.1 directory creation from the browser | `TestDirectoryUIShowsCreateForm`, `TestDirectoryUIPostCreatesChildAndRedirects`, `TestDirectoryUIPostCreatesRootChild`, `TestDirectoryUIPostRejectsInvalidAndExistingNames` |
| §44/§44.1–§44.3 directory-tree upload | `TestDirectoryZipUploadPreservesPathsAndRejectsTraversal`, `TestDirectoryZipRejectsCompressedAndEntryCountLimits`, `TestDirectoryZipRejectsTotalExpandedSize`, `TestDirectoryZipRejectsExpandedFileLimit`, `TestZipResponseBodyAndNoTopLevelStripping`, `TestZipEdgeRejections` |
| §42/§55 Markdown subset, safety and UI escaping | `TestMarkdownSubsetRendering`, `TestMarkdownAssetContract`, `TestRecordValuesEscapedInUI`, `TestHiddenFieldsOmittedFromUI` |
| §46 help, §47 health, home and favicon | `TestHelpAndHealthPagesUseWorkspaceLayout`, `TestHomePageAndFavicon`, `TestHelpFormats`, `TestHealthCountsAndVersion`, `TestMethodRestrictionsAndReservedRoutes` |
| §49/§50/§16 API shapes, requests and errors | `TestAPIRequestAndQueryErrors`, `TestAPIErrorCodesAndHeaders`, `TestMetadataShapes` |
| §52 SQLite indexing | `TestAutomaticAndExplicitIndexesAreAppliedAndReconciled`, `TestOpenDatabaseAddsIndexMetadataToExistingSchema`, `TestDeclaredIndexesAreNonUniqueAndValidated`, `TestIndexReconciliationSurvivesReopen` |
| §56 observability and redaction | `TestHTTPFailureLoggingOmitsRequestDetails` |
| §59/§60 examples and repository deliverables | `TestExampleTableMetadataAndRecords`, `TestMarkdownAndClientRenderingExamplesPublish` |
| §62 lifecycle and graceful shutdown | `TestServeUntilSignalGracefullyDrainsActiveRequest` |
| §54 authentication and transport | `TestLoadAuthConfigDefaultsAndValidation`, `TestAuthenticationProtectsAllApplicationRoutes`, `TestHTTPSRequirementAndTrustedNetworks`, `TestForwardedHTTPSOnlyTrustedFromProxy` |
| §43 ClioJS browser client | `TestClioJSAssetsAndHelp`, `TestClioJSBehaviorWithNode` |
| §35 collection data browser | `TestCollectionBrowserRoutes` |
| §65.1–§65.2/§65.5/§65.7, §66.3–§66.4 projects model, API, storage scope and reserved names | `TestProjectCreateListReadDelete`, `TestProjectReservedNamesAndValidation`, `TestProjectDeleteRejectsNonEmpty`, `TestHealthReportsProjectCount`, `TestOpenDatabaseMigratesLegacyTablesToProjectScope` |
| §65.3 project data scoping and isolation | `TestProjectDataIsolation` |
| §66.1/§66.3/§66.5–§66.6 project-first routing, instance routes and the data partition | `TestBareRoutesRedirectToDefaultProject`, `TestUnknownProjectReturnsNotFound`, `TestProjectScopedHumanAndAPIRoutes` |

## Verification commands

```sh
go test ./...
go vet ./...
go build -buildvcs=false -o /tmp/gocl-clio-check .
```

Building requires Go 1.25 and a C toolchain for the cgo SQLite driver. The
ClioJS behaviour test additionally runs Node.js when it is available and skips
otherwise.

## Pending conformance (spec v1.5)

Section 64 of [`SPEC.md`](SPEC.md) is normative but not yet implemented. The
areas below have no automated coverage yet; each row must move into the table
above as its tests land. Until then, the implementation does not conform to
spec v1.5.

| Spec area | Planned tests |
| --- | --- |
| §64.2 file identity and reconciliation | stable IDs across replace, rename and move; rescan add/remove/refresh; an on-disk rename yields a new ID |
| §64.3/§64.5 stable URLs and serving | `/f/{id}` and `/f/{id}/{name}`; download disposition and nosniff; byte ranges |
| §64.4 filesystem REST API | list and directory listing; create/replace (`PUT`), directory create, delete, `move`, `copy`, `rescan`; content transfer and ranges; conflict rules; size limit |
| §64.6 extraction and text index | text-like extraction; PDF text; size cap; rebuild by rescan |
| §64.7 search API | ranking and snippets; literal-term safety; paging; empty/oversized query |
| §64.8 agent enrichment | fingerprint match/mismatch; survives move; delete falls back to native |
| §64.9 attachment fields | validation on create/update/default; delete-integrity `409`; record representation |
| §64.10 WebDAV | optional flag; method set including MOVE/COPY; ID preservation; auth enforcement |
| §64.11 backup and restore | restored database plus content reproduces IDs, timestamps and agent text |
| §64.14 web file explorer | `/files` shell and reserved route; ClioJS `Clio.FileBrowser`; breadcrumbs and URL state; upload/rename/delete actions; referenced-entry `409`; escaped search snippets |
| §65 namespaces (projects) | remaining: project-scoped content/files; project-relative paths and `(project, path)` uniqueness; a per-project content subtree |
| §66 project-scoped URL scheme and partitions | remaining: the unified files partition (`PUT`/`DELETE`/`GET /files`, `/{project}/files/id/{id}`, WebDAV at both `/api/v1/{project}/files/dav` and `/{project}/files/dav`); Page and Directory APIs replaced |
