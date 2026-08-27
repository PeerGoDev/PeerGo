# PeerGo 参考实现检出清单

本目录只用于架构研究、协议核对和基准测试，不是 PeerGo 的源码依赖，也不得从 PeerGo 运行时代码中 import、链接或调用。各项目保留自己的 Git 历史和许可证；PeerGo 根仓库通过 `.gitignore` 忽略这些嵌套仓库，只跟踪本清单。

首次检出时间：2026-08-22（Asia/Shanghai）；新增项目按下表记录当前检出提交。所有仓库均以 `--depth 1 --filter=blob:none` 从清单所列分支拉取。

| 本地目录 | 上游 | 分支 | 检出提交 |
|---|---|---|---|
| `unit3d` | <https://github.com/HDInnovations/UNIT3D> | `master` | `8b88f4c8182eb3d3912ffef425c3224dcfd596f4` |
| `nexusphp` | <https://github.com/xiaomlove/nexusphp> | `php8` | `310e24d0a9ef4822218e2604e71682f7bf25bb64` |
| `torrust-tracker` | <https://github.com/torrust/torrust-tracker> | `develop` | `e92f28f2a9e2bb72c4922a8db532041cd5759d2c` |
| `torrust-index` | <https://github.com/torrust/torrust-index> | `develop` | `843aafff6b459a9ade4097273fbc430b7ecb959e` |
| `aquatic` | <https://github.com/greatest-ape/aquatic> | `master` | `a2ddc4b323c5aaf844ce32b655b0ffc8c4836cde` |
| `reunit3d-announce` | <https://github.com/ReUnit3d/ReUnit3d-Announce> | `main` | `fcd189e9aebf07a12a1f8cc39500819c3870925b` |
| `pt-depiler` | <https://github.com/pt-plugins/PT-depiler> | `master` | `47f7d05b99d2c198b8fca676a861fdd0d763a0f2` |

## 更新方式

参考目录是可替换的本地研究资料。更新单个项目时，在对应目录执行 `git fetch --depth 1 origin <branch>`，核对变更后再 fast-forward；更新完成后必须同步修改本清单中的提交，并重新审阅 PeerGo `README.md` 中受影响的结论。

不要把这些仓库作为 Git submodule 提交，也不要复制其实现代码到 PeerGo。若确需采用代码，必须先核对许可证、记录 ADR，并以独立依赖或合规重写方式引入。
