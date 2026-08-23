---
name: commit-and-push-each-feature
description: 用户要求每完成一个独立功能就创建提交并立即推送远程仓库
metadata: 
  node_type: memory
  type: feedback
  originSessionId: bdb276f4-4447-46d6-9156-fa3d71548296
---

每完成一个独立功能需求，都要在验证通过后创建独立 Git 提交，并立即推送到当前跟踪的远程分支。

**Why:** 用户希望远程仓库持续保留细粒度、可追踪的功能进度。

**How to apply:** 按功能边界精确暂存文件，排除构建产物和无关工作区改动；提交后立即 `git push`，再开始下一个功能。
