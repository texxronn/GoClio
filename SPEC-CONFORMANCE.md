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
| §36–§41 content tree, directories, pages | `TestFilesDirectoryCreation`, `TestFilesDirectoryListAndDelete`, `TestFilePageRoundTripAndPathSafety`, `TestFilesPageCreateReplaceDeleteAndValidation`, `TestContentPathValidation`, `TestContentResourceIdentity`, `TestDirectoryChildrenAndContentUI` |
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
| §35 collection data browser (re-scoped to `/{project}/data`) | `TestDataBrowserRoutes` |
| §65.1–§65.2/§65.5/§65.7, §66.3–§66.4 projects model, API, storage scope and reserved names | `TestProjectCreateListReadDelete`, `TestProjectReservedNamesAndValidation`, `TestProjectDeleteRejectsNonEmpty`, `TestHealthReportsProjectCount`, `TestOpenDatabaseMigratesLegacyTablesToProjectScope` |
| §64.2 content entries and identity | `TestContentEntryIdentityStableAcrossReplace`, `TestContentReconciliationAddsAndRemoves`, `TestContentEntriesMigrationFromPageTimes`, `TestContentLayoutMigrationMovesLegacyRoot`, `TestFilesCRUDByPathAndID`, `TestFilesMoveAndCopy` |
| §64.4 filesystem REST API | `TestFilesCRUDByPathAndID`, `TestFilesDirectoryListingAndPageKind`, `TestFilesDirectoryCreation`, `TestFilesListFiltersAndPaging`, `TestFilesMoveAndCopy`, `TestFilesConflictsAndReservedSegment`, `TestFilesDeleteByPathRemovesSubtree`, `TestFilesUploadLimitAndTraversal`, `TestFilesRescanSummary`, `TestFilesProjectIsolation` |
| §64.3/§64.5/§66.9 stable URLs and file serving | `TestFileContentDownloadHeadersAndRanges`, `TestStableHumanURLDownloadsRawPageBytes`, `TestPathURLNonPageDownloads`, `TestFileContentMissingIDReturnsNotFound`, `TestFileContentIsProjectScoped`, `TestStableHumanURLMissingAndMethodRestrictions` |
| §64.6 native extraction and the text index | `TestNativeExtractionPerType`, `TestNativeExtractionCapsIndexedText`, `TestFTS5Available`, `TestContentSearchIndexesWritesAndDrops`, `TestContentSearchRebuiltByRescan`, `TestContentSearchMoveKeepsIDAndPath`, `TestContentSearchProjectScoped`, `TestFilesRepresentationReportsIndexedState` |
| §64.7 search API | `TestSearchReturnsRankedResultsAndShape`, `TestSearchSnippetsArePlainTextWithSentinels`, `TestSearchPagingIsDeterministic`, `TestSearchRejectsEmptyAndOversizedAndBadPaging`, `TestSearchTreatsOperatorsAsLiterals`, `TestBuildMatchQueryQuotesAndPrefix`, `TestSearchIsProjectScoped`, `TestSearchReportsIndexSourceAndContentType` |
| §64.8 agent enrichment API | `TestEnrichmentRequiresMatchingFingerprint`, `TestEnrichmentAcceptsSha256Fingerprint`, `TestEnrichmentByIDPathSegment`, `TestEnrichmentValidatesRequestShape`, `TestEnrichmentDeleteFallsBackToNative`, `TestEnrichmentSurvivesMove`, `TestEnrichmentIsProjectScoped`, `TestEnrichmentEditedFileInvalidatesAgentText` |
| §64.9 attachment fields | `TestAttachmentFieldNormalizationAndMetadata`, `TestAttachmentValidationOnCreateUpdateAndDefault`, `TestAttachmentDefaultValidation`, `TestAttachmentRejectsCrossProjectEntries`, `TestAttachmentDeleteIntegrity`, `TestAttachmentDeleteIntegrityDirectorySubtree`, `TestAttachmentSurvivesRenameAndMove`, `TestAttachmentFilterSortAndDistinct`, `TestAttachmentFormAndRecordRendering` |
| §64.10/§66.8 WebDAV mount | `TestWebDAVDisabledByDefault`, `TestWebDAVMethodSet`, `TestWebDAVMovePreservesIDAndCopyAssignsNewID`, `TestWebDAVDirectoryMovePreservesDescendantIDs`, `TestWebDAVDeleteReferencedReturnsConflict`, `TestWebDAVRequiresAuthentication`, `TestWebDAVPropfindReturnsEntryMetadata` |
| §57/§64.11/§65.8 backup and restore | `TestBackupRestoreReproducesIdentityTimestampsAndEnrichment`, `TestRestoreReconcilesContentTree`, `TestBackupRefusesNonEmptyDestination`, `TestBackupRequiresExistingDatabase`, `TestRestoreValidatesManifest`, `TestRestoreRefusesNonEmptyTargetUnlessForced`, `TestBackupRestoreCommands` |
| §64.14 web file explorer and human search surface | `TestFileExplorerShellRoutes`, `TestFileExplorerLightActions`, `TestSearchUIWorkflow`, `TestClioJSBehaviorWithNode` |
| §65.3 project data scoping and isolation | `TestProjectDataIsolation`, `TestContentIsolationBetweenProjects` |
| §66.1/§66.3/§66.5–§66.6 project-first routing, instance routes, human URLs and the data partition | `TestBareRoutesRedirectToDefaultProject`, `TestUnknownProjectReturnsNotFound`, `TestProjectScopedHumanAndAPIRoutes`, `TestDataBrowserRoutes`, `TestFileExplorerShellRoutes`, `TestSearchUIWorkflow` |
| §66.2/§66.7 files partition replaces the Page and Directory APIs | `TestLegacyPageAndDirectoryRoutesRemoved`, `TestFilesPageCreateReplaceDeleteAndValidation`, `TestFilePageRoundTripAndPathSafety`, `TestFilesDirectoryCreation` |

## Verification commands

```sh
go test -tags sqlite_fts5 ./...
go vet -tags sqlite_fts5 ./...
go build -tags sqlite_fts5 -buildvcs=false -o /tmp/gocl-clio-check .
```

Building requires Go 1.25 and a C toolchain for the cgo SQLite driver. The
`sqlite_fts5` tag is mandatory: the full-text index uses SQLite FTS5
(section 64.6), which the driver only compiles in under that tag. The ClioJS
behaviour test additionally runs Node.js when it is available and skips
otherwise.

## Pending conformance

None: every normative area exercised by this implementation has automated
coverage in the table above.
