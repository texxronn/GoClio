# Clio conformance matrix

This file maps the normative areas of [`SPEC.md`](SPEC.md) to the automated tests
that exercise them, plus the verification commands. It exists so that a spec
change can be checked against concrete coverage rather than a prose checklist.

Tests are Go tests in the repository root unless noted. Tests exercise behaviour
through HTTP and use temporary SQLite databases and content directories.

| Spec area | Representative tests |
| --- | --- |
| §8–§14 data model, tables, fields, records | `TestMetadataAndRecordCRUDValidation`, `TestTableSchemaUpdateProtectsStoredData` |
| §13.2/§61.14 references and uniqueness | `TestReferencePreventsDeletingReferencedRecord`, `TestAddingUniqueConstraintWithDuplicatesRollsBackSchemaUpdate` |
| §18/§19 table creation and schema updates | `TestExampleTableMetadataAndRecords`, `TestMetadataAndRecordCRUDValidation`, `TestTableSchemaUpdateProtectsStoredData` |
| §22–§28 paging, sorting, filtering, distinct, grouping, aggregation | `TestRecordQueriesFilteringSortingPagingDistinctAndAggregates`, `TestQueryPageKeepsCorrectTopRowsAndOrdering` |
| §23 deterministic ordering and bounded memory | `TestSQLQueryPagesBoundApplicationAllocations`, `records_benchmark_test.go` |
| §61.10 exact numeric values and collations | `TestSQLQueriesPreserveExactNumericSemantics`, `TestQuerySQLCollationsPreserveTypedOrdering` |
| §12/§29/§61.21–61.23 time-series ranges and buckets | `TestTimeseriesNormalizesUTCAndBucketsWeeks`, `TestTimeseriesTableUITimeRangeAndBuckets` |
| §32–§34 table UI and forms | `TestTableUIRootGroupEditDeleteAndErrors`, `TestTableUIGroupingAggregationFilteringAndPaging`, `TestGenericTableFormsAndOperationalPages` |
| §35–§40 content tree, directories, pages | `TestDirectoryAPICreationStillWorks`, `TestDirectoryAPIListAndDelete`, `TestPageAPIRoundTripAndPathSafety`, `TestPageAPIUpdateDeleteAndValidation` |
| §38/§61.4 directory creation from the browser | `TestDirectoryUIShowsCreateForm`, `TestDirectoryUIPostCreatesChildAndRedirects`, `TestDirectoryUIPostCreatesRootChild`, `TestDirectoryUIPostRejectsInvalidAndExistingNames` |
| §42/§61.7–61.9 directory-tree upload | `TestDirectoryZipUploadPreservesPathsAndRejectsTraversal`, `TestDirectoryZipRejectsCompressedAndEntryCountLimits`, `TestDirectoryZipRejectsTotalExpandedSize`, `TestDirectoryZipRejectsExpandedFileLimit` |
| §44 help, §45 health, home and favicon | `TestHelpAndHealthPagesUseWorkspaceLayout`, `TestHomePageAndFavicon` |
| §47/§52 API requests, queries and errors | `TestAPIRequestAndQueryErrors` |
| §49 SQLite indexing | `TestAutomaticAndExplicitIndexesAreAppliedAndReconciled`, `TestOpenDatabaseAddsIndexMetadataToExistingSchema` |
| §53 observability and redaction | `TestHTTPFailureLoggingOmitsRequestDetails` |
| §56 examples and repository deliverables | `TestExampleTableMetadataAndRecords`, `TestMarkdownAndClientRenderingExamplesPublish` |
| §59 lifecycle and graceful shutdown | `TestServeUntilSignalGracefullyDrainsActiveRequest` |
| §62 authentication and transport | `TestLoadAuthConfigDefaultsAndValidation`, `TestAuthenticationProtectsAllApplicationRoutes`, `TestHTTPSRequirementAndTrustedNetworks`, `TestForwardedHTTPSOnlyTrustedFromProxy` |
| §63 ClioJS browser client | `TestClioJSAssetsAndHelp`, `TestClioJSBehaviorWithNode` |
| §64 collection data browser | `TestCollectionBrowserRoutes` |

## Verification commands

```sh
go test ./...
go vet ./...
go build -buildvcs=false -o /tmp/gocl-clio-check .
```

Building requires Go 1.25 and a C toolchain for the cgo SQLite driver. The
ClioJS behaviour test additionally runs Node.js when it is available and skips
otherwise.
