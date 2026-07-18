# 城市模拟 F7.3 地块、分区与建筑事实底座详细设计

版本：v1.0（2026-07-18）  
目标引擎：`city-f7-v2`  
前置引擎：`city-f7-v1`  
状态：已实现并通过后端、迁移、重放恢复、前端与生产构建验收

## 1. 目标与边界

F7.3 把 F3 仅存在于 district 的住宅/商业/工业聚合容量，拆成有稳定空间身份、几何、用途、容量和占用链的真实地块与建筑投影。完成后，住房供给不再只是一列数字；后续公共设施、通勤、企业选址和开发项目可引用同一套地块与建筑 ID。

本阶段实现：

1. 世界绑定的土地规则与固定 hash；
2. 可追溯地块、分区规则、建筑 footprint、楼层、聚合单位池和 portal；
3. household cohort 到住宅单位池的初始占用分配；
4. F3 district 容量到 F7 明细容量的精确兼容桥；
5. `city-f7-v2` 创世与 `city-f7-v1 → city-f7-v2` 审计升级；
6. 土地规范状态、快照、逐 tick 重放、投影恢复和授权 bbox 查询；
7. CLASSIC renderer 的 structure/portal 只读叠层。

本阶段不实现：

- 开发申请、审批、施工、拆除和动态改建；这些属于 F7.4。
- 商业/工业企业选址和租约；这些属于 F7.5。
- 土地买卖、按揭、利率和证券。
- 任意多边形 GIS、连续海拔或倾斜三维镜头。
- 客户端随机生成房屋。

## 2. 关键决策

### 2.1 土地是规范事实，不是 Chunk 装饰

F7.1 Chunk payload 保持不可变基础地形。地块和建筑使用独立版本化投影叠加，不重写首次生成的 Mapgen payload：

```text
immutable base Chunk
  + immutable F7.3 land baseline
  + future posted land/construction mutations
  = current semantic scene
```

这避免升级土地规则时静默改变历史 Chunk hash，也允许恢复器分别校验基础地图和建筑状态。

### 2.2 首版几何是整数网格矩形

每个 parcel/building footprint 使用：

```text
chunk_x, chunk_y, z
local_min_x, local_min_y
local_max_x, local_max_y
```

范围均为 `0..31`，边界包含端点。首版只允许单 Chunk 轴对齐矩形；稳定 ID 和 API 预留未来由多个 footprint part 组成的扩展，但没有实际需要前不引入通用 GIS 引擎。

### 2.3 地图比例与法定面积分离

CLASSIC Cell 是可玩拓扑单位，不假装是精确地籍测量。`nominal_cell_area_sqm` 只用于视图比例和初始 footprint 估算；parcel 的 `area_sqm`、建筑 `footprint_area_sqm` 与 `floor_area_sqm` 是权威整数面积。

每个 district 的 parcel `area_sqm` 总和必须精确等于现有 `developable_area_units`。几何格数和法定面积都保留，不能用浮点比例互相反推并覆盖。

### 2.4 住宅单位采用聚合池

首版不为数万套住宅各建一行。`city_building_unit_pools` 以建筑/用途为粒度保存：

- `unit_count`
- `occupied_unit_count`
- `capacity_units_per_unit`
- `version`

`city_housing_allocations` 把 household cohort 的 `household_units` 分配到住宅池。后续只有需要产权、租约或室内玩法时，才从池中实例化永久 unit；聚合到实例化必须守恒。

## 3. 固定规则集

首版土地规则：`sub2api-land@1.0.0`。规则内容进入固定 SHA-256，并由世界 profile 绑定。

| zone | primary use | FAR milli | coverage milli | max floors | 每容量面积 |
| --- | --- | ---: | ---: | ---: | ---: |
| `residential` | residential | 3000 | 450 | 12 | 90 m²/住宅单位 |
| `commercial` | commercial | 4000 | 600 | 16 | 25 m²/容量单位 |
| `industrial` | industrial | 1500 | 700 | 4 | 40 m²/容量单位 |

所有比例使用整数 milli；面积与容量使用 `int64`。楼层数按向上取整计算，并同时受 FAR、覆盖率和最大楼层约束。任何容量无法落入合法 footprint 时，创世/升级整体失败，不能截断容量。

## 4. 确定性基线生成

输入固定为：

```text
city-f7-v2
world seed
F7.1 spatial ruleset hash
immutable Overmap root hash
sub2api-land@1.0.0 hash
district code/order/area/developable area/三类容量
household cohort stable key + household_units
```

生成步骤：

1. 按 `(chunk_y, chunk_x)` 读取 immutable Overmap；排除 river/deep-water Tile。
2. 每个可开发 Tile 建立四个道路安全象限地块，固定避开中心 `13..18` 走廊。
3. 四象限用途固定为住宅、住宅、商业、工业，确保每个有可开发 Tile 的 district 都具备三类用途。
4. district `developable_area_units` 按稳定地块顺序做商与余数分配，和保持精确。
5. 每类 district 容量按同用途地块稳定分配，和必须精确等于 F3 聚合容量。
6. 每个地块建立一栋初始建筑和一个同用途单位池；楼层/面积由绑定规则计算。
7. 每栋建筑建立显式入口；多层建筑按相邻 Z 建立双向 stair portal。
8. household cohort 按 district、收入带顺序装入住宅池，分配量必须等于 `household_units`，且池占用不超过容量。
9. 对全部规则、地块、建筑、池、portal 和 allocation 的规范数组计算 baseline hash。

稳定 code 不含数据库 ID：

```text
parcel_{district}_{x-token}_{y-token}_{quadrant}
building_{district}_{x-token}_{y-token}_{quadrant}
pool_{building-code}
```

负坐标 token 使用 `n4`，非负使用 `p0`，避免符号和排序歧义。

## 5. 数据模型

### 5.1 `city_land_profiles`

每世界一行：规则 ID/版本/hash、F7.1 Overmap root、nominal cell area、baseline hash、各对象计数、revision 和 metadata。普通运行不可修改。

### 5.2 `city_zoning_rules`

保存世界绑定规则快照：用途、FAR、覆盖率、最大楼层、每容量面积和状态。这样历史世界不跟随部署默认规则静默改变。

### 5.3 `city_parcels`

保存 stable code、district、Chunk/local 矩形、zone、权威面积、状态和版本。首版一个地块只在一个 Chunk；未来多 part 扩展必须发布新版本。

### 5.4 `city_buildings`

保存 parcel、stable code、用途、footprint、`base_z/top_z/floor_count`、权威 footprint/floor area、容量/占用、质量、完成 tick、状态和版本。拆除未来只进入终态，不删除历史。

### 5.5 `city_building_unit_pools`

保存建筑内聚合单位库存。住宅池的 `unit_count` 与建筑住宅容量相等；商业/工业池保存对应容量单位，首版占用为零。

### 5.6 `city_housing_allocations`

保存住宅池与 household cohort 的聚合占用关系。相同 pool/cohort 只有一行，`allocated_units > 0`。它是当前投影；F7.4/F7.5 的动态变化必须由不可变 occupancy fact 推进。

### 5.7 `city_building_portals`

保存 entrance/stair 的 from/to XYZ、方向性、状态和版本。跨层路径只能读取 portal，不能因为同一 XY 自动跨 Z。

### 5.8 `city_land_baselines`

每世界一条不可变基线事实：tick、规则 hash、baseline hash、对象计数、metadata 和 `posted_at`。创世为 tick 0；升级为当前 tick 的同 tick 新版本基线。

## 6. 数据库硬约束

提交前 `assert_city_land_foundation(world_id)` 至少验证：

1. `city-f7-v2` 世界恰有一个 profile 和一个 posted baseline；
2. profile、baseline 和明细计数一致；
3. zoning 规则恰好匹配绑定规则快照；
4. parcel 面积按 district 汇总等于 `developable_area_units`；
5. building 与 parcel 同 world/district，footprint 包含于 parcel；
6. building 容量按 district/use 汇总精确等于 F3 三类容量；
7. floor count、Z 范围、coverage、FAR、occupied/capacity 全部合法；
8. unit pool 容量与 building 容量一致，occupied 汇总与 building 一致；
9. housing allocation 只指向住宅池，按 pool 汇总等于占用；
10. 按 cohort 汇总等于 `household_units`；
11. portal 坐标属于对应 building/parcel，stair 只连接相邻 Z；
12. baseline hash 格式和所有 stable code 唯一。

所有表的 INSERT/UPDATE/DELETE 都需要事务内写闸门。正常运行只有创世/升级和未来 land mutation reducer 能开启；recovery 只能在已验证 replay 的恢复事务内开启。baseline 封账后不能普通更新或删除。

## 7. 版本与兼容

- `city-f7-v1` canonical JSON 必须逐字节保持原形，不增加 `land` 字段。
- `city-f7-v2` 在 `spatial` 后增加 `land` 规范状态，但引擎 stage 顺序仍是 demography → spatial/land → markets。
- F7.1 Mapgen 绑定继续使用 `city-f7-v1` 作为生成域；升级到 v2 不改变 Overmap、Chunk proof 或 payload hash。
- v1→v2 只允许 paused、无 pending command、源快照已验证的 owner 操作。
- dry-run 在事务回滚前生成完整 land canonical 和目标 hash；apply 保存同 tick v2 upgrade snapshot。
- 新世界直接创建 v2 profile、land baseline 和 genesis snapshot。

## 8. 规范状态

`city-f7-v2` 新增：

```json
{
  "land": {
    "profile": {},
    "zoning_rules": [],
    "parcels": [],
    "buildings": [],
    "unit_pools": [],
    "housing_allocations": [],
    "portals": []
  }
}
```

排序固定：zone code；parcel/building/pool stable code；allocation 按 district、cohort stable key、pool code；portal 按 building code、from z、code。规范状态不包含数据库 ID、创建时间或查询顺序。

## 9. Replay 与 Recovery

F7.3 基线在创世/升级 snapshot 中，因此 v2 replay 从任意 v2 snapshot 开始时直接继承土地状态。F7.3 没有动态土地命令，后续 tick 的 land reducer 是无变化验证；F7.4 加入 mutation 后再扩展 reducer。

Recovery：

1. 从 snapshot 读取 land profile 和规范明细；
2. 使用 seed、immutable Overmap、规则 hash 和 district/cohort 状态重新生成期望基线；
3. baseline hash 或规范明细不一致立即失败；
4. 在 recovery gate 内对 profile、规则、parcel、building、pool、allocation、portal 做精确集合恢复；
5. 再执行数据库 assertion，重载 live canonical，必须与 snapshot 逐字节相同。

恢复不能根据已漂移的 land 投影决定目标，也不能跳过 F3/F6 容量和家庭守恒。

## 10. 授权读取 API

```text
GET /api/v1/city/worlds/:world_id/land
  ?min_x=&max_x=&min_y=&max_y=&z=
```

响应返回 profile、zone 目录和 bbox 内 parcel/building/unit pool/portal；住宅 allocation 只返回聚合 cohort stable key 和数量，不暴露不存在的个人身份。bbox 每轴最多 9，必须裁剪到 Overmap；世界成员授权先于查询。

不存在 F7.3 profile 的 v1 世界返回稳定 `CITY_LAND_STATE_NOT_FOUND`，不隐式升级。

## 11. CLASSIC 显示

renderer-neutral projection 在 F7.1 terrain/furniture 上叠加：

```text
portal > building edge/door > building floor > furniture > terrain
```

- Overmap 以 parcel/use 分区细边框和建筑数量摘要表示，不替换原始地形。
- Local 在当前 Z 绘制真实 footprint；地表显示入口，多层显示 stair portal。
- 选择 Cell 时检查器同时列出 parcel、building、unit pool、占用和 portal。
- TILES 仅在 CLASSIC 语义与检查结果测试稳定后增加；两种 renderer 消费同一 projection。

## 12. 验收门禁

1. 默认规则固定黄金 hash；相同 seed/Overmap/district/cohort 跨数据库 ID 生成相同 baseline hash。
2. 所有 district 的 parcel 面积、三类 building 容量和 cohort 占用精确守恒。
3. 负 Chunk 坐标、象限 footprint、Z/portal 边界和整数向上取整测试通过。
4. v1 canonical 历史黄金不变；v1→v2 dry-run 不落库，apply 同 tick 建基线。
5. 升级不改变 F7.1 Overmap root、Chunk root、generation proof 或 payload hash。
6. profile/baseline/规则/parcel/building/pool/allocation/portal 直接写入被数据库拒绝。
7. v2 创世、snapshot、逐 tick replay、投影漂移诊断和 verified recovery 闭环。
8. bbox、授权、响应上限和 v1 state-not-found 错误语义测试通过。
9. CLASSIC structure/portal 与 API 检查器显示同一事实；不生成客户端建筑。
10. 全量 Go、迁移静态测试、PostgreSQL 集成、前端测试、typecheck 和 production build 通过。

只有以上门禁全部通过，F7.3 才能标记完成并进入 F7.4 动态开发项目。
