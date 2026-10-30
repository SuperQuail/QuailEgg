import { defineConfig } from '@rspress/core';

export default defineConfig({
  root: 'docs',
  // GitHub Pages 项目站点挂在 /<仓库名>/ 下
  base: '/QuailEgg/',
  lang: 'zh',
  title: 'QuailEgg',
  description: '变更记录驱动的自动发版工具',
  themeConfig: {
    socialLinks: [
      { icon: 'github', mode: 'link', content: 'https://github.com/SuperQuail/QuailEgg' },
    ],
    editLink: {
      docRepoBaseUrl: 'https://github.com/SuperQuail/QuailEgg/tree/main/site/docs',
      text: '在 GitHub 上编辑此页',
    },
    nav: [
      { text: '指南', link: '/guide/introduction', activeMatch: '^/guide/' },
      { text: '参考', link: '/reference/cli', activeMatch: '^/reference/' },
      { text: '更新日志', link: 'https://github.com/SuperQuail/QuailEgg/blob/main/CHANGELOG.md' },
    ],
    sidebar: {
      '/guide/': [
        {
          text: '开始',
          items: [
            { text: '简介', link: '/guide/introduction' },
            { text: '快速上手', link: '/guide/quick-start' },
          ],
        },
        {
          text: '基础',
          items: [
            { text: '变更记录', link: '/guide/change-records' },
            { text: '配置文件', link: '/guide/configuration' },
            { text: '发布流程', link: '/guide/release-flow' },
          ],
        },
        {
          text: '自动化',
          items: [
            { text: 'GitHub Actions', link: '/guide/github-actions' },
          ],
        },
        {
          text: '进阶',
          items: [
            { text: '预发布', link: '/guide/prereleases' },
            { text: '多包仓库', link: '/guide/monorepos' },
            { text: '多语言生态', link: '/guide/ecosystems' },
            { text: '发布资产', link: '/guide/assets' },
          ],
        },
        {
          text: '帮助',
          items: [
            { text: '问题排查', link: '/guide/faq' },
          ],
        },
      ],
      '/reference/': [
        {
          text: '参考',
          items: [
            { text: '命令行', link: '/reference/cli' },
            { text: '配置项', link: '/reference/config' },
            { text: '变更记录格式', link: '/reference/change-record' },
          ],
        },
      ],
    },
  },
});
