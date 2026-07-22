-- City worlds are now canonical shared worlds. Membership, not a per-owner
-- private-city limit, determines who can enter each world.
DROP INDEX IF EXISTS idx_city_worlds_one_private_active_per_owner;
