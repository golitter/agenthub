module.exports = {
  extends: ['@commitlint/config-conventional'],
  rules: {
    // 强制 scope 不能为空
    'scope-empty': [2, 'never'],
    // 强制 scope 只能是以下枚举值之一（或者多范围组合）
    'scope-enum': [
      2,
      'always',
      [
        'frontend', // 前端
        'backend', // 后端
        'agentend', // agent端
        'common', // 公共
        'docs', // 文档
        'other', // 其他
      ],
    ],
  },
};
