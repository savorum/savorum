import { isolate } from "../lib/direction.ts"
import { m } from "../lib/messages.ts"

/** Prefer translated codes; retain specific source diagnostics when no structured translation exists. */
export function problemMessage(
  code: string,
  status: number,
  fallback: string,
  params: Record<string, unknown> = {},
  diagnostic = "",
): string {
  if (status === 507) return m.api_error_storage_capacity()
  const hasDiagnostic = diagnostic.trim() !== "" && !/^[\w:-]+$/.test(diagnostic)
  if (
    code === "export-asset-unavailable" &&
    typeof params.recipe_title === "string" &&
    typeof params.reason_code === "string" &&
    params.reason_code
  ) {
    return m.api_export_asset_unavailable({
      title: isolate(params.recipe_title),
      reason: params.reason_code,
    })
  }
  if (
    [
      "import-invalid",
      "import-preview-invalid",
      "export-source-invalid",
      "export-too-large",
      "recipe-already-here",
    ].includes(code) &&
    hasDiagnostic
  )
    return diagnostic

  switch (code) {
    case "recent-authentication-required":
      return m.api_problem_recent_authentication_required()
    case "invalid-credentials":
      return m.api_problem_invalid_credentials()
    case "account-conflict":
      return m.api_problem_account_conflict()
    case "token-scope-required":
      return m.api_problem_token_scope_required()
    case "tokens-unavailable":
      return m.api_problem_tokens_unavailable()
    case "origin-required":
    case "invalid-origin":
      return m.api_problem_origin_required()
    case "administrator-required":
      return m.api_problem_administrator_required()
    case "library-membership-required":
      return m.api_problem_library_membership_required()
    case "authentication-required":
      return m.api_problem_authentication_required()
    case "csrf-required":
      return m.api_problem_csrf_required()
    case "deletion-confirmation-required":
      return m.api_problem_deletion_confirmation_required()
    case "local-mode-enabled":
      return m.api_problem_local_mode_enabled()
    case "invalid-role":
      return m.api_problem_invalid_role()
    case "invalid-audit-filter":
      return m.api_problem_invalid_audit_filter()
    case "passkeys-unavailable":
      return m.api_problem_passkeys_unavailable()
    case "public-url-unavailable":
      return m.api_problem_public_url_unavailable()
    case "account-unavailable":
    case "libraries-unavailable":
    case "session-unavailable":
      return m.api_problem_account_unavailable()

    case "access-denied":
      return m.api_problem_access_denied()
    case "asset-derivative-unavailable":
      return m.api_problem_asset_derivative_unavailable()
    case "asset-invalid":
      return m.api_problem_asset_invalid()
    case "asset-not-found":
      return m.api_problem_asset_not_found()
    case "cooking-invalid":
      return m.api_problem_cooking_invalid()
    case "export-source-empty":
      return m.api_problem_export_source_empty()
    case "export-source-invalid":
      return m.api_problem_export_source_invalid()
    case "export-too-large":
      return m.api_problem_export_too_large()
    case "import-invalid":
      return m.api_problem_import_invalid()
    case "import-job-not-found":
      return m.api_problem_import_job_not_found()
    case "import-job-not-resumable":
      return m.api_problem_import_job_not_resumable()
    case "import-preview-busy":
      return m.api_problem_import_preview_busy()
    case "import-preview-expired":
      return m.api_problem_import_preview_expired()
    case "import-preview-invalid":
      return m.api_problem_import_preview_invalid()
    case "import-preview-stale":
      return m.api_problem_import_preview_stale()
    case "import-preview-too-large":
      return m.api_problem_import_preview_too_large()
    case "import-state-conflict":
      return m.api_problem_import_state_conflict()
    case "internal-error":
      return m.api_problem_internal_error()
    case "invalid-if-match":
      return m.api_problem_invalid_if_match()
    case "organization-invalid":
      return m.api_problem_organization_invalid()
    case "organization-state-conflict":
      return m.api_problem_organization_state_conflict()
    case "precondition-required":
      return m.api_problem_precondition_required()
    case "print-artifact-invariant":
      return m.api_problem_print_artifact_invariant()
    case "print-busy":
      return m.api_problem_print_busy()
    case "print-document-not-found":
      return m.api_problem_print_document_not_found()
    case "print-render-failed":
      return m.api_problem_print_render_failed()
    case "print-request-timeout":
      return m.api_problem_print_request_timeout()
    case "print-source-ambiguous":
      return m.api_problem_print_source_ambiguous()
    case "print-source-empty":
      return m.api_problem_print_source_empty()
    case "print-timeout":
      return m.api_problem_print_timeout()
    case "print-unsupported-characters":
      return m.api_problem_print_unsupported_characters()
    case "recipe-already-here":
      return m.api_problem_recipe_already_here()
    case "recipe-conflict":
      return m.api_problem_recipe_conflict()
    case "recipe-document-identity-change":
      return m.api_problem_recipe_document_identity_change()
    case "recipe-invalid":
      return m.api_problem_recipe_invalid()
    case "recipe-not-found":
      return m.api_problem_recipe_not_found()
    case "recipe-refresh-stale":
      return m.api_problem_recipe_refresh_stale()
    case "recipe-request-invalid":
      return m.api_problem_recipe_request_invalid()
    case "recipe-revision-conflict":
      return m.api_problem_recipe_revision_conflict()
    case "recipe-source-baseline-missing":
      return m.api_problem_recipe_source_baseline_missing()
    case "recipe-state-conflict":
      return m.api_problem_recipe_state_conflict()
    case "revision-conflict":
      return m.api_problem_revision_conflict()
    case "search-invalid":
      return m.api_problem_search_invalid()
    case "service-unavailable":
      return m.api_problem_service_unavailable()
    case "source-not-found":
      return m.api_problem_source_not_found()
    case "sref-invalid":
      return m.api_problem_sref_invalid()
  }
  // Application and source-adapter diagnostics may contain recipe titles and
  // actionable causes unavailable in older or newer clients. Do not discard them.
  if (code && code !== "about:blank" && hasDiagnostic) return diagnostic
  switch (status) {
    case 400:
    case 422:
      return m.api_error_invalid()
    case 401:
      return m.api_error_authentication()
    case 403:
      return m.api_error_forbidden()
    case 404:
      return m.api_error_not_found()
    case 409:
    case 412:
    case 428:
      return m.api_error_conflict()
    case 413:
      return m.api_error_too_large()
    case 429:
      return m.api_error_rate_limit()
    default:
      return status >= 500 ? m.api_error_unavailable() : fallback
  }
}

export function violationMessage(code: string): string {
  switch (code) {
    case "amount.invalid":
      return m.api_violation_amount_invalid()
    case "amount.range_order":
      return m.api_violation_amount_range_order()
    case "amount.required":
      return m.api_violation_amount_required()
    case "amount.shape":
      return m.api_violation_amount_shape()
    case "asset.path":
      return m.api_violation_asset_path()
    case "asset.path_duplicate":
      return m.api_violation_asset_path_duplicate()
    case "author.kind":
      return m.api_violation_author_kind()
    case "author.kind_label":
      return m.api_violation_author_kind_label()
    case "author.url":
      return m.api_violation_author_url()
    case "choice.shape":
      return m.api_violation_choice_shape()
    case "chronology.impossible":
      return m.api_violation_chronology_impossible()
    case "chronology.invalid":
      return m.api_violation_chronology_invalid()
    case "duration.invalid":
      return m.api_violation_duration_invalid()
    case "duration.opaque_approximate":
      return m.api_violation_duration_opaque_approximate()
    case "duration.opaque_exclusive":
      return m.api_violation_duration_opaque_exclusive()
    case "duration.range_incomplete":
      return m.api_violation_duration_range_incomplete()
    case "duration.required":
      return m.api_violation_duration_required()
    case "duration.value_exclusive":
      return m.api_violation_duration_value_exclusive()
    case "extension.name":
      return m.api_violation_extension_name()
    case "id.duplicate":
      return m.api_violation_id_duplicate()
    case "id.invalid":
      return m.api_violation_id_invalid()
    case "language.invalid":
      return m.api_violation_language_invalid()
    case "list.duplicate":
      return m.api_violation_list_duplicate()
    case "list.too_long":
      return m.api_violation_list_too_long()
    case "normalization.method":
      return m.api_violation_normalization_method()
    case "normalization.review":
      return m.api_violation_normalization_review()
    case "normalization.source":
      return m.api_violation_normalization_source()
    case "normalization.warning":
      return m.api_violation_normalization_warning()
    case "quantity.kind":
      return m.api_violation_quantity_kind()
    case "quantity.options":
      return m.api_violation_quantity_options()
    case "quantity.shape":
      return m.api_violation_quantity_shape()
    case "quantity.terms":
      return m.api_violation_quantity_terms()
    case "source.application":
      return m.api_violation_source_application()
    case "source.empty":
      return m.api_violation_source_empty()
    case "source.identity":
      return m.api_violation_source_identity()
    case "source.retrieved_at":
      return m.api_violation_source_retrieved_at()
    case "source.url":
      return m.api_violation_source_url()
    case "sref_opaque.address":
      return m.api_violation_sref_opaque_address()
    case "sref_opaque.detached":
      return m.api_violation_sref_opaque_detached()
    case "sref_opaque.shape":
      return m.api_violation_sref_opaque_shape()
    case "sref_opaque.too_many":
      return m.api_violation_sref_opaque_too_many()
    case "temperature.opaque_exclusive":
      return m.api_violation_temperature_opaque_exclusive()
    case "temperature.purpose.kind":
      return m.api_violation_temperature_purpose_kind()
    case "temperature.purpose.kind_required":
      return m.api_violation_temperature_purpose_kind_required()
    case "temperature.purpose.label":
      return m.api_violation_temperature_purpose_label()
    case "text.control_character":
      return m.api_violation_text_control_character()
    case "text.required":
      return m.api_violation_text_required()
    case "text.too_long":
      return m.api_violation_text_too_long()
    case "timing_assertion.attention":
      return m.api_violation_timing_assertion_attention()
    case "timing_assertion.kind":
      return m.api_violation_timing_assertion_kind()
    case "timing_assertion.label_required":
      return m.api_violation_timing_assertion_label_required()
    case "timing_assertion.provenance":
      return m.api_violation_timing_assertion_provenance()
    case "timing_assertion.source":
      return m.api_violation_timing_assertion_source()
    case "unit.catalog":
      return m.api_violation_unit_catalog()
    case "unit.id":
      return m.api_violation_unit_id()
    case "unit.physical_required":
      return m.api_violation_unit_physical_required()
    case "unit.temperature_context":
      return m.api_violation_unit_temperature_context()
    case "unit.temperature_required":
      return m.api_violation_unit_temperature_required()
    case "variant.text_required":
      return m.api_violation_variant_text_required()
    case "variants.too_many":
      return m.api_violation_variants_too_many()
    default:
      return m.api_error_field_invalid()
  }
}
