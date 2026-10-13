ALTER TABLE recipe_user_state ADD COLUMN preferred_yield_assertion INTEGER
    CHECK (preferred_yield_assertion IS NULL OR preferred_yield_assertion >= 0);
