//go:build linux

// TestParity_* wrappers for FR-077's implementation-phase matrix. Each row's
// test cell names one thin seam; keep the two in sync when coverage moves.

package cli

import "testing"

func TestParity_P_CMD_001_list(t *testing.T) {
	TestAC019ListRowsFormatSortLocalAndAttributes(t)
}

func TestParity_P_CMD_002_search(t *testing.T) {
	TestAC019SearchMatchesNamesThenShortDescUnlessNames(t)
}

func TestParity_P_CMD_003_show(t *testing.T) {
	TestAC020ShowFieldOrderAbsentPropertiesAndLongListWrapping(t)
}

func TestParity_P_CMD_004_policy(t *testing.T) {
	TestAC021PolicyGlobalViewRepositoryPrecedenceSignaturesAndPins(t)
}

func TestParity_P_CMD_005_clean(t *testing.T) {
	TestAC016Clean(t)
}

func TestParity_P_CMD_006_download(t *testing.T) {
	TestDownloadWritesArchiveAndSignatureToCwd(t)
}

func TestParity_P_CMD_007_history(t *testing.T) {
	TestHistoryListAndDetail(t)
}

func TestParity_P_CMD_008_fetch(t *testing.T) {
	TestAC035FetchWritesSyncsAndCleanFetchReverts(t)
}

func TestParity_P_CMD_009_update(t *testing.T) {
	TestAC009UpdateSyncsThenCountsWithoutSummaryOrPrompt(t)
}

func TestParity_P_CMD_010_upgrade(t *testing.T) {
	TestAC008UpgradeFamilyArgv(t)
}

func TestParity_P_CMD_011_full_upgrade(t *testing.T) {
	TestFullUpgradeConfigChangesNothing(t)
}

func TestParity_P_CMD_012_safe_upgrade(t *testing.T) {
	TestAC008UpgradeFamilyArgv(t)
}

func TestParity_P_CMD_013_install(t *testing.T) {
	TestAC005InstallArgvAndReinstallGuard(t)
}

func TestParity_P_CMD_014_remove(t *testing.T) {
	TestAC006RemoveFiltersAndPurgeArgv(t)
}

func TestParity_P_CMD_015_purge(t *testing.T) {
	TestAC006RemoveFiltersAndPurgeArgv(t)
}

func TestParity_P_CMD_016_autoremove(t *testing.T) {
	TestAC007AutoremoveAndAutopurgeArgv(t)
}

func TestParity_P_CMD_017_autopurge(t *testing.T) {
	TestAC007AutoremoveAndAutopurgeArgv(t)
}

func TestParity_P_CMD_018_moo(t *testing.T) {
	TestSurfaceMooAndNoSubcommand(t)
}

func TestParity_P_OPT_001_v_verbose(t *testing.T) {
	TestAC024VerbosePrintsEachCommand(t)
}

func TestParity_P_OPT_002_debug(t *testing.T) {
	TestConfigDebugImpliesVerboseAndDumpsResolvedConfig(t)
}

func TestParity_P_OPT_003_config_file(t *testing.T) {
	TestConfigFlagReplacesBothLayers(t)
}

func TestParity_P_OPT_004_history_dir_dir(t *testing.T) {
	TestHistoryDirPrecedenceThroughBinary(t)
}

func TestParity_P_OPT_005_tui(t *testing.T) {
	TestS1GateMatrixPTY(t)
}

func TestParity_P_OPT_006_no_tui(t *testing.T) {
	TestS1GateMatrixPTY(t)
}

func TestParity_P_OPT_007_o_option(t *testing.T) {
	TestSurfaceNAFlagsRefuseBeforeConfigAndChildren(t)
}

func TestParity_P_OPT_008_color_when(t *testing.T) {
	TestIntegrationConfigColorModeEndToEnd(t)
}

func TestParity_P_OPT_009_no_color(t *testing.T) {
	TestPrecedenceColorFlagAndNoColor(t)
}

func TestParity_P_OPT_010_d_download_only(t *testing.T) {
	TestDownloadOnlyStopsAfterTheCacheFill(t)
}

func TestParity_P_OPT_011_simple(t *testing.T) {
	TestPlainLineGolden(t)
}

func TestParity_P_OPT_012_update(t *testing.T) {
	TestIntegrationUpdateAlwaysSyncs(t)
}

func TestParity_P_OPT_013_no_update(t *testing.T) {
	TestIntegrationAC008SuppressAndConflict(t)
}

func TestParity_P_OPT_014_allow_unauthenticated(t *testing.T) {
	TestSurfaceNAFlagsRefuseBeforeConfigAndChildren(t)
}

func TestParity_P_OPT_015_y_assume_yes(t *testing.T) {
	TestAC006YesSkipsThePrompt(t)
}

func TestParity_P_OPT_016_n_assume_no(t *testing.T) {
	TestAC011AssumeNo(t)
}

func TestParity_P_OPT_017_remove_essential(t *testing.T) {
	TestSurfaceNAFlagsRefuseBeforeConfigAndChildren(t)
}

func TestParity_P_OPT_018_purge(t *testing.T) {
	TestAC006RemoveFiltersAndPurgeArgv(t)
}

func TestParity_P_OPT_019_f_fix_broken(t *testing.T) {
	TestSurfaceNAFlagsRefuseBeforeConfigAndChildren(t)
}

func TestParity_P_OPT_020_no_fix_broken(t *testing.T) {
	TestSurfaceNoOpNegativesAddNothingToArgv(t)
}

func TestParity_P_OPT_021_install_recommends(t *testing.T) {
	TestSurfaceNAFlagsRefuseBeforeConfigAndChildren(t)
}

func TestParity_P_OPT_022_no_install_recommends(t *testing.T) {
	TestSurfaceNoOpNegativesAddNothingToArgv(t)
}

func TestParity_P_OPT_023_install_suggests(t *testing.T) {
	TestSurfaceNAFlagsRefuseBeforeConfigAndChildren(t)
}

func TestParity_P_OPT_024_no_install_suggests(t *testing.T) {
	TestSurfaceNoOpNegativesAddNothingToArgv(t)
}

func TestParity_P_OPT_025_a_all_versions(t *testing.T) {
	TestAC019AllVersionsInstalledUpgradableVirtual(t)
}

func TestParity_P_OPT_026_a_all_arches(t *testing.T) {
	TestAC019AllArchesNoOpNamesOnlyBracesVersionAndArch(t)
}

func TestParity_P_OPT_027_auto_remove_autoremove(t *testing.T) {
	TestAC013ImplicitAutoremove(t)
}

func TestParity_P_OPT_028_no_auto_remove_no(t *testing.T) {
	TestAC013ImplicitAutoremove(t)
}

func TestParity_P_OPT_029_pkg_names(t *testing.T) {
	TestSurfaceArityCountsPackageWordsOnly(t)
}

func TestParity_P_OPT_030_description(t *testing.T) {
	TestAC019SummaryDescriptionCompositionAndOrGlobs(t)
}

func TestParity_P_OPT_031_summary(t *testing.T) {
	TestAC019SummaryDescriptionCompositionAndOrGlobs(t)
}

func TestParity_P_OPT_032_i_installed(t *testing.T) {
	TestAC019AllVersionsInstalledUpgradableVirtual(t)
}

func TestParity_P_OPT_033_u_upgradable(t *testing.T) {
	TestAC019UpgradableAllVersionsEmitsOnlyPlanRows(t)
}

func TestParity_P_OPT_034_v_virtual(t *testing.T) {
	TestAC019AllVersionsInstalledUpgradableVirtual(t)
}

func TestParity_P_OPT_035_m_machine(t *testing.T) {
	TestAC022MachineRowsAreNDJSONSchemaOne(t)
}

func TestParity_P_OPT_036_n_names_names_only(t *testing.T) {
	TestAC019SearchMatchesNamesThenShortDescUnlessNames(t)
}

func TestParity_P_OPT_037_lists(t *testing.T) {
	TestAC016Clean(t)
}

func TestParity_P_OPT_038_fetch_clean(t *testing.T) {
	TestCleanFetchRestoresTheBackupAndDeletesTheManifest(t)
}

func TestParity_P_OPT_039_non_free(t *testing.T) {
	TestFetchNonFreeWritesTheDerivedRepositoryFile(t)
}

func TestParity_P_OPT_040_https_only(t *testing.T) {
	TestFetchHTTPSOnlyRewritesTheProbedAndShownURL(t)
}

func TestParity_P_OPT_041_sources(t *testing.T) {
	TestFetchNARefusalsMakeNoNetworkOrFileAccess(t)
}

func TestParity_P_OPT_042_auto_n(t *testing.T) {
	TestFetchAutoTwoValidatesTwoFastest(t)
}

func TestParity_P_OPT_043_c_country(t *testing.T) {
	TestFetchCountryFiltersBeforeProbe(t)
}

func TestParity_P_OPT_044_debian(t *testing.T) {
	TestFetchNARefusalsMakeNoNetworkOrFileAccess(t)
}

func TestParity_P_OPT_045_ubuntu(t *testing.T) {
	TestFetchNARefusalsMakeNoNetworkOrFileAccess(t)
}

func TestParity_P_OPT_046_devuan(t *testing.T) {
	TestFetchNARefusalsMakeNoNetworkOrFileAccess(t)
}

func TestParity_P_OPT_047_remove_config(t *testing.T) {
	TestSurfaceNAFlagsRefuseBeforeConfigAndChildren(t)
}

func TestParity_P_OPT_048_reinstall(t *testing.T) {
	TestAC005InstallArgvAndReinstallGuard(t)
}

func TestParity_P_OPT_049_t_target_release(t *testing.T) {
	TestSurfaceNAFlagsRefuseBeforeConfigAndChildren(t)
}

func TestParity_P_OPT_050_print_uris(t *testing.T) {
	TestPrintURIsEmitsOneObjectPerPlannedDownload(t)
}

func TestParity_P_OPT_051_exclude(t *testing.T) {
	TestSurfaceNAFlagsRefuseBeforeConfigAndChildren(t)
}

func TestParity_P_OPT_052_full(t *testing.T) {
	TestFullUpgradeConfigChangesNothing(t)
}

func TestParity_P_OPT_053_no_full(t *testing.T) {
	TestFullUpgradeConfigChangesNothing(t)
}

func TestParity_P_OPT_054_safe(t *testing.T) {
	TestFullUpgradeConfigChangesNothing(t)
}

func TestParity_P_OPT_056_history_clear_all(t *testing.T) {
	TestAC029ClearOneAndAll(t)
}

func TestParity_P_OPT_057_id_or_last(t *testing.T) {
	TestHistoryListAndDetail(t)
}

func TestParity_P_OPT_058_moo_help(t *testing.T) {
	TestSurfaceMooAndNoSubcommand(t)
}

func TestParity_P_OPT_059_moo_update(t *testing.T) {
	TestSurfaceMooAndNoSubcommand(t)
}

func TestParity_P_OPT_060_moo_no_update(t *testing.T) {
	TestSurfaceMooAndNoSubcommand(t)
}

func TestParity_P_CFG_001_nala_auto_remove(t *testing.T) {
	TestAC013ImplicitAutoremove(t)
}

func TestParity_P_CFG_002_nala_auto_update(t *testing.T) {
	TestUpgradeHonoursTheSyncRules(t)
}

func TestParity_P_CFG_003_nala_update_show_packages(t *testing.T) {
	TestAC009UpdateShowPackages(t)
}

func TestParity_P_CFG_004_nala_full_upgrade(t *testing.T) {
	TestFullUpgradeConfigChangesNothing(t)
}

func TestParity_P_CFG_005_nala_simple(t *testing.T) {
	TestPlainLineGolden(t)
}

func TestParity_P_CFG_006_nala_assume_yes(t *testing.T) {
	TestAC019ConfigAssumeYesSkipsPrompt(t)
}

func TestParity_P_CFG_007_ui_mode(t *testing.T) {
	TestSummaryScreenGateMatrix(t)
}

func TestParity_P_CFG_008_ui_unit(t *testing.T) {
	TestIntegrationConfigUnitEndToEnd(t)
}

func TestParity_P_CFG_009_color_mode(t *testing.T) {
	TestIntegrationConfigColorModeEndToEnd(t)
}

func TestParity_P_CFG_021_toml_outside_the_supported(t *testing.T) {
	TestTOMLOutsideSubsetExitsTwo(t)
}

func TestParity_P_VIS_001_nine_role_default_palette(t *testing.T) {
	TestSGRPrefix(t)
}

func TestParity_P_VIS_002_section_rule(t *testing.T) {
	TestRuleWidthFallsBackFromTTYToColumns(t)
}

func TestParity_P_VIS_011_confirmation_prompt(t *testing.T) {
	TestAC010PromptOverAPTY(t)
}

func TestParity_P_VIS_012_nothing_to_do(t *testing.T) {
	TestAC011NothingToDo(t)
}

func TestParity_P_VIS_013_progress_widget_frame_and(t *testing.T) {
	TestProgressViewsFixedHeightsAndTitles(t)
}

func TestParity_P_VIS_014_progress_label_and_percent(t *testing.T) {
	TestProgressDividerPercentTruncatesFillRounds(t)
}

func TestParity_P_VIS_015_progress_bar(t *testing.T) {
	TestWidgetFillRoundsAndPercentTruncates(t)
}

func TestParity_P_VIS_016_widget_info_and_message(t *testing.T) {
	TestRenderDownloadView80(t)
}

func TestParity_P_VIS_017_update_view_updating(t *testing.T) {
	TestUpdateMessageHeaderIsRegular(t)
}

func TestParity_P_VIS_018_mirror_score_finished(t *testing.T) {
	TestAC046MirrorScoreView(t)
}

func TestParity_P_VIS_019_plain_progress_line(t *testing.T) {
	TestPlainLineGolden(t)
}

func TestParity_P_VIS_020_discovery_row_format_and(t *testing.T) {
	TestAC019ListRowsFormatSortLocalAndAttributes(t)
}

func TestParity_P_VIS_021_show_and_policy_label(t *testing.T) {
	TestAC021PolicyGlobalViewRepositoryPrecedenceSignaturesAndPins(t)
}

func TestParity_P_VIS_022_policy_marker(t *testing.T) {
	TestAC021PolicyPackageViewInstalledCandidateAndVersionTable(t)
}

func TestParity_P_VIS_023_prefixes_and_roles(t *testing.T) {
	TestAC015FailureGetsHint(t)
}

func TestParity_P_VIS_024_download_completion_block(t *testing.T) {
	TestDownloadWritesArchiveAndSignatureToCwd(t)
}

func TestParity_P_VIS_025_machine_json_never_coloured(t *testing.T) {
	TestAC022ShowMachineCarriesPropertiesAndNoANSI(t)
}

func TestParity_P_VIS_026_simple_layout(t *testing.T) {
	TestPlainLineGolden(t)
}

func TestParity_P_VIS_030_download_view_mirror_table(t *testing.T) {
	parityDownloadNoMirrorTable(t)
}

func TestParity_P_VIS_031_fetched_completion_line(t *testing.T) {
	parityUpdateNoFetched(t)
}

func TestParity_P_VIS_032_nothing_to_fetch(t *testing.T) {
	parityUpdateNoNothingToFetch(t)
}

func TestParity_P_VIS_033_dpkg_conffile_and_status(t *testing.T) {
	TestAC048ChildOutputForwardedVerbatim(t)
}

func TestParity_P_TUI_001_transaction_summary_screen(t *testing.T) {
	TestS1FrameBytesAndCancel(t)
}

func TestParity_P_TUI_002_history_detail_screen(t *testing.T) {
	TestHistoryDetailScreenFrame(t)
}

func TestParity_P_TUI_003_fetch_picker(t *testing.T) {
	TestAC052FetchPickerFrame(t)
}

func TestParity_P_TUI_004_inline_progress_viewport(t *testing.T) {
	TestAC046WidgetOverPTY80(t)
}

func TestParity_P_TUI_005_release_info(t *testing.T) {
	TestUpdateNeverOpensAReleaseScreen(t)
}

func TestParity_P_BEH_001_root_requirement_and_refusal(t *testing.T) {
	TestAC001NonRootSummaryHintAndExit(t)
}

func TestParity_P_BEH_002_exit_code_set(t *testing.T) {
	TestExitCodePassThrough(t)
}

func TestParity_P_BEH_003_stream_split_and_durable(t *testing.T) {
	TestAC048StdoutCarriesOnlyDurableOutput(t)
}

func TestParity_P_BEH_004_prompt_set_and_answers(t *testing.T) {
	TestAC003PromptAnswers(t)
}

func TestParity_P_BEH_005_nothing_to_do_and(t *testing.T) {
	TestAC011NothingToDo(t)
}

func TestParity_P_BEH_006_history_store_and_schema(t *testing.T) {
	TestHistorySuccessfulInstallWritesSchemaOneEntry(t)
}

func TestParity_P_BEH_007_history_list_and_detail(t *testing.T) {
	TestHistoryListAndDetail(t)
}

func TestParity_P_BEH_008_history_undo(t *testing.T) {
	TestAC026UndoInstallRemovesForcedAndWritesOneReplayEntry(t)
}

func TestParity_P_BEH_009_history_redo(t *testing.T) {
	TestAC028RedoReinstallsAtRecordedVersion(t)
}

func TestParity_P_BEH_010_history_clear(t *testing.T) {
	TestAC029ClearOneAndAll(t)
}

func TestParity_P_BEH_011_legacy_history_json_import(t *testing.T) {
	parityNoLegacyHistoryJSON(t)
}

func TestParity_P_BEH_013_download_to_the_current(t *testing.T) {
	TestDownloadWritesArchiveAndSignatureToCwd(t)
}

func TestParity_P_BEH_016_fetch_write_and_revert(t *testing.T) {
	TestFetchWritesMaskingFileBackupManifestAndSyncs(t)
}

func TestParity_P_BEH_018_machine_output_for_show(t *testing.T) {
	TestAC022ShowMachineCarriesPropertiesAndNoANSI(t)
}

func TestParity_P_BEH_019_machine_output_for_policy(t *testing.T) {
	TestAC022PolicyGlobalMachineObject(t)
}

func TestParity_P_BEH_020_print_uris_json(t *testing.T) {
	TestPrintURIsEmitsOneObjectPerPlannedDownload(t)
}

func TestParity_P_BEH_021_machine_schema_identity(t *testing.T) {
	TestAC022MachineRowsAreNDJSONSchemaOne(t)
}

func TestParity_P_BEH_022_early_update_and_auto(t *testing.T) {
	TestUpgradeHonoursTheSyncRules(t)
}

func TestParity_P_BEH_023_implicit_autoremove_second_transaction(t *testing.T) {
	TestAC013ImplicitAutoremove(t)
}

func TestParity_P_BEH_024_sync_step(t *testing.T) {
	TestIntegrationAC008UpgradeSyncsSeparately(t)
}

func TestParity_P_BEH_025_n_a_flag_contract(t *testing.T) {
	TestSurfaceNAFlagsRefuseBeforeConfigAndChildren(t)
}

func TestParity_P_BEH_026_non_tty_refusal(t *testing.T) {
	TestAC012NonTTYExactMessage(t)
}

func TestParity_P_BEH_027_colour_precedence(t *testing.T) {
	TestPrecedenceColorFlagAndNoColor(t)
}

func TestParity_P_BEH_028_dynamic_completion_protocol(t *testing.T) {
	parityNoDynamicCompletionProtocol(t)
}

func TestParity_P_BEH_029_i18n(t *testing.T) {
	TestAC018VerboseShowsPassSetAndLocale(t)
}

func TestParity_P_BEH_030_config_migration(t *testing.T) {
	TestLegacyAllTenKeysRunWarnAndAreNeverRewritten(t)
}

func TestParity_P_BEH_032_o_apt_option_passthrough(t *testing.T) {
	TestSurfaceNAFlagsRefuseBeforeConfigAndChildren(t)
}

func TestParity_P_BEH_033_moo_and_the_no(t *testing.T) {
	TestSurfaceMooAndNoSubcommand(t)
}

func TestParity_P_BEH_034_config_files_only_replay(t *testing.T) {
	TestUndoUpgradeRestoresExactOldVersionFromCache(t)
}

func TestParity_P_BEH_038_apt_auth_config(t *testing.T) {
	parityNoAuthFileRead(t)
}

func TestParity_P_BEH_039_q_to_cancel_key(t *testing.T) {
	TestInlineViewportCtrlCExits130AndTearsDown(t)
}

func TestParity_P_BEH_040_direct_deb_url_install(t *testing.T) {
	parityDebNeverInstalls(t)
}

func TestParity_P_PKG_001_debian_packaging_files(t *testing.T) {
	parityNoDebianPackaging(t)
}

func TestParity_P_PKG_002_apt_sources_written_by(t *testing.T) {
	TestFetchWritesMaskingFileBackupManifestAndSyncs(t)
}

func TestParity_P_PKG_004_multi_suite_ppas(t *testing.T) {
	parityNoPpaArtifacts(t)
}

func TestParity_P_PKG_005_nala_conf_shipped_in(t *testing.T) {
	parityTemplateHasNoEtcConfig(t)
}
