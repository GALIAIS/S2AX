# F9.3.0 / V19 空间交通网络底座

版本：v1.0（2026-07-20）
状态：V19/F9.3.0 已完成；F9.3.1 动态基础设施层待实现

## 1. 目的与边界

V9 已经提供了可回放的设施—区域—中心换乘宏观图、容量预约和路线事实，但它刻意不宣称自己是道路、站点或货运设施的空间表示。V19 是 F9.3 的第一块正式底座：将每个已封存 V9 hub/edge 映射为一个不可变的空间网络节点/走廊，并把世界生成 profile 对应的交通风格内容包一同封存。

这解决了两类问题：

1. 后续道路等级、站点、换乘、货运终端、施工、维护和灾害不能直接依赖 V9 的抽象 `tier` 字段；它们需要独立、稳定且可审计的基础设施身份。
2. 中国都市、日本都市和默认温带城市的交通形态必须由内容 profile 选择，不能在 reducer 中出现按国家分支的业务逻辑。

V19 **不**替换 V9 路由器、不重写已存在的 V9 demand/route/allocation，也不声称已经实现逐车道寻路、列车时刻表、车队、车票、运费或道路建设。它是下一层系统的稳定输入，而不是把静态标签误包装为完整交通仿真。

## 2. 前置与兼容

- 新世界默认版本为 `city-openworld-v19`；它完整保留 V7–V18 的状态和语义。
- `city-openworld-v18 → city-openworld-v19` 只能在 paused world 进行。升级 tick 成为 V19 baseline，既有 V9 图被静态映射，不补造历史的设施建设、路径使用或拥堵事实。
- V19 的空间网络只消费 V9 已冻结的 hub/edge 和 `city_open_world_bindings` 中已经封存的 worldgen profile 绑定；运行中部署的新 profile 绝不能改变既有 V19 world。
- V18 的批次货运和 V17 的 receipt gate 保持原有因果顺序。V19 仅给其 V9 edge 增加可引用的 corridor identity，不给予库存、现金或交付新语义。

## 3. 内容包：交通风格而非国家分支

`cityspatial` 增加独立的 `OpenWorldTransportStyleProfile` catalog。每个条目绑定一个既有 worldgen profile ID，拥有独立的 profile ID、版本和 SHA-256 content hash，并至少声明：

- `interchange`、`zone`、`facility` 三类 hub 的空间节点类别；
- `walk`、`transit`、`freight` 三种模式在 `local` 与 `trunk` tier 上的 corridor class；
- 与 worldgen profile 的关联，而不是以国家名称驱动 reducer。

首批 catalog：

| worldgen profile | transport style | 例子 |
| --- | --- | --- |
| `sub2api-temperate-openworld` | 默认都会交通 | city interchange / local street / freight arterial |
| `jp.metropolitan` | 日本都市交通 | station concourse / arcade walkway / rail trunk / service alley |
| `cn.metropolitan` | 中国都市交通 | metro transfer / plaza walkway / boulevard transit / industrial arterial |

未来增加 profile 只增加内容条目与新版本/hash；不得改写已发布条目，也不得用 `if country == ...` 改变已有 world 的输出。

## 4. 权威数据模型

### 4.1 Profile

`city_open_world_spatial_network_profiles` 每世界一行，保存：

- V19 profile/version/content hash/baseline；
- V9 edge → spatial corridor、worldgen transport style、immutable static topology 三个固定 contract；
- transport style ID/version/hash；
- 源 worldgen profile ID/version/hash；
- node/corridor 安全上限、实际数量、revision 和最小 metadata。

profile 是规范状态。它不能在运行中更新；任何扩容、道路施工或网络改造必须由后续 engine version 的 append-only fact/transition 处理。

### 4.2 Node

`city_open_world_spatial_network_nodes` 是 V9 hub 的一对一投影：

- 稳定 code、源 hub code/kind、风格 node class；
- 已封存 anchor `(x,y,z)`；
- definition version/content hash/metadata。

节点不是数据库 ID 的别名。所有下游命令或 UI 使用稳定 public code，且一个源 hub 在一个 V19 world 内只能有一个 node。

### 4.3 Corridor

`city_open_world_spatial_network_corridors` 是 V9 directed edge 的一对一投影：

- 稳定 code、源 edge code、mode、起终 node；
- style 解析出的 corridor class、V9 tier；
- distance、base travel tick、capacity 与 definition hash。

一条 corridor 仍是 V9 的宏观走廊，不是地图 cell 的路径列表。后续 F9.3.1 可在不改变 corridor identity 的前提下追加细分 segment、站台、信号和可用容量状态。

## 5. 生成与不变量

初始化或 paused upgrade 的确定性步骤：

```text
冻结的 CityOpenWorldBinding
        + 冻结的 V9 hubs/edges
        + 绑定的 transport style profile
                      │
                      ▼
    stable hub-code sort → nodes
    stable edge-code sort → corridors
                      │
                      ▼
    profile + node/corridor content hashes
```

关键不变量：

1. 每个 V9 hub 恰好对应一个 V19 node；每个 V9 edge 恰好对应一个 V19 corridor。
2. node 的 source hub kind/class/anchor 与 V9 及 transport style 一致。
3. corridor 的 source edge/mode/tier/端点/distance/travel/capacity 与 V9 一致，且 class 必须由 frozen transport style 的 `(mode,tier)` 解析。
4. profile 的世界生成 binding 与实际 `city_open_world_bindings` 一致；style hash 和 profile hash 可重新计算。
5. node/corridor 代码、definition hash 和 metadata 都是确定性的；重复 source、断开的 endpoint、未知 style class 或计数漂移均为 invariant failure。
6. V19 不允许通过普通 runtime、管理 UI 或 SQL 直接更新/删除这些静态投影。只有 bootstrap、审计 upgrade 和 verified recovery 有受限写路径。

## 6. Canonical、回放、恢复与版本向量

`CityOpenWorldSpatialNetworkState` 与 V7–V18 sibling state 一样进入 runtime canonical snapshot；排序固定为 node code、corridor code。它不包含 V9 的 mutable demand/route history，而是通过 source public code 绑定 V9 既有 canonical state。

- replay 在每个 checkpoint 验证 V19 state 的静态 identity 与 V9 source topology；任一静态值漂移立即停止。
- recovery 先恢复 V9，再恢复 V19 profile/node/corridor，最后运行 SQL foundation assertion。
- V19 version vector 新增 transport-network content catalog binding。该 binding 只封存 style policy/contract，不把可变 node/corridor count 当成部署内容。

## 7. 读取接口与权限

```text
GET /api/v1/city/worlds/:world_id/open-world/spatial-network
```

活跃 world member 可读取节点/走廊的公共 code、类别、坐标、容量和 topology metadata，用于地图和交通分析；不返回数据库主键、Actor 控制数据、上游账号、钱包、订单私有 payload 或其他企业敏感信息。V19 state 没有按企业归属的字段，因此无需 V15 双边过滤。

## 8. 验收

1. 三个 worldgen profile 都能解析独立 transport style；同输入重复解析得到同一 SHA-256 hash。
2. 新 V19 world 生成的 node/corridor 与 V9 hub/edge 一一对应，且日本/中国 profile 给出不同的 node/corridor class。
3. V18→V19 paused upgrade 不创建 demand、route、allocation、shipment、receipt、inventory 或 ledger 事实。
4. 破坏 source hub、source edge、style hash、端点、class、数量或 content hash 均被 Go validator/SQL assertion 拒绝。
5. snapshot/replay/recovery 与 version-vector 验证在 PostgreSQL 集成链中恢复同一 V19 state。
6. 只读 API 的非成员访问被拒绝，成员访问不泄露任何 V15/V16/V17/V18 私有数据。

## 9. 后续明确边界

V20/F9.3.1 才可追加 corridor segment、站点/终端容量、可维护资产和道路建设/关闭事实；V21+ 才可让 V9/V20 调度器消费这些动态容量。部分收货、货损、拒收、承运责任、运费和保险仍属于 F10.3+，必须以新的资源 operation/ledger facts 表达，不能复用 V19 静态 corridor 直接改写 V15 交付。
