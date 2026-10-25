# 普通用户引导 Driver 副本

来源：npm driver.js 1.4.0，MIT 许可见 LICENSE。原文件摘要与补丁次数在 build-driver.mjs 固定校验。

cleanup.1 和 host-pointer.1 原样迁移自只读原型 main @ 1e5f678 的 tools/driver_bundle.py；prefix.1 将类名和 ID 改成 s2o-driver-，不影响管理员使用的 npm 模块。

在项目根执行 `node frontend/src/features/guide/driver/build-driver.mjs` 生成。生成结果提交到 Git；不修改 node_modules。driver.spec.ts 在内存重建并逐字节比较。
