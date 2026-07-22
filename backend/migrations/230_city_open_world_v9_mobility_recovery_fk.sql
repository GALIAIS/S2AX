-- The V9 route/demand relationship is a deliberate deferred cycle. PostgreSQL
-- treats ON DELETE RESTRICT as immediate even when the foreign key itself is
-- declared DEFERRABLE, which prevents a recovery transaction from clearing the
-- two projections before rebuilding them. NO ACTION preserves the same
-- referential guarantee at commit while allowing both sides to be removed and
-- restored atomically.

ALTER TABLE city_open_world_mobility_routes
    DROP CONSTRAINT IF EXISTS city_open_world_mobility_route_demand_fk;
ALTER TABLE city_open_world_mobility_routes
    ADD CONSTRAINT city_open_world_mobility_route_demand_fk
        FOREIGN KEY (demand_id, world_id)
        REFERENCES city_open_world_mobility_demands(id, world_id) ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE city_open_world_mobility_demands
    DROP CONSTRAINT IF EXISTS city_open_world_mobility_demand_route_fk;
ALTER TABLE city_open_world_mobility_demands
    ADD CONSTRAINT city_open_world_mobility_demand_route_fk
        FOREIGN KEY (route_id, world_id)
        REFERENCES city_open_world_mobility_routes(id, world_id) ON DELETE NO ACTION
        DEFERRABLE INITIALLY DEFERRED;
