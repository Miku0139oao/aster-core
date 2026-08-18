import { defineConfig } from "vitepress";
import { fileURLToPath } from "node:url";

const base = process.env.DOCS_BASE || "/";
const publicDir = fileURLToPath(
  new URL("./public-runtime", import.meta.url),
);

const zhDescription =
  "Aster Core 繁體中文入門教學：連接代理節點、AnyTLS + REALITY、規則、DNS、TUN 與故障排查";
const enDescription =
  "Aster Core English docs: connect a proxy node, AnyTLS + REALITY, rules, DNS, TUN, and troubleshooting";

export default defineConfig({
  title: "Aster Core",
  description: zhDescription,
  base,
  sitemap: {
    hostname: "https://astercore.fubukishop.app",
  },
  cleanUrls: true,
  lastUpdated: true,
  appearance: true,
  vite: {
    publicDir,
  },
  head: [
    ["link", { rel: "icon", type: "image/png", href: `${base}logo.png` }],
    ["meta", { name: "theme-color", content: "#5b5bd6" }],
    ["meta", { property: "og:type", content: "website" }],
  ],
  markdown: {
    lineNumbers: true,
  },
  locales: {
    root: {
      label: "繁體中文",
      lang: "zh-TW",
      title: "Aster Core",
      description: zhDescription,
      head: [
        ["meta", { property: "og:locale", content: "zh_TW" }],
        ["meta", { property: "og:title", content: "Aster Core 文件" }],
        ["meta", { property: "og:description", content: zhDescription }],
      ],
      themeConfig: {
        siteTitle: "Aster Core 文件",
        outline: {
          level: [2, 3],
          label: "本頁內容",
        },
        docFooter: {
          prev: "上一頁",
          next: "下一頁",
        },
        lastUpdated: {
          text: "最後更新",
          formatOptions: {
            dateStyle: "medium",
            timeStyle: "short",
          },
        },
        returnToTopLabel: "回到頂部",
        sidebarMenuLabel: "選單",
        darkModeSwitchLabel: "外觀",
        lightModeSwitchTitle: "切換至淺色模式",
        darkModeSwitchTitle: "切換至深色模式",
        langMenuLabel: "切換語言",
        nav: [
          { text: "首頁", link: "/" },
          {
            text: "實戰教學",
            items: [
              { text: "教學總覽", link: "/tutorials/" },
              { text: "第一個代理設定", link: "/tutorials/first-proxy" },
              { text: "路由與 DNS", link: "/tutorials/routing-dns" },
              { text: "AnyTLS + REALITY", link: "/tutorials/anytls-reality" },
              { text: "使用者與訂閱（服務端）", link: "/tutorials/user-management" },
              { text: "Linux VPS（服務端）", link: "/tutorials/linux-production" },
              { text: "故障排查", link: "/tutorials/troubleshooting" },
            ],
          },
          { text: "與 Mihomo 的差異", link: "/reference/mihomo-differences" },
          { text: "OpenWrt Kernel DIRECT", link: "/deployment/openwrt" },
          { text: "效能基準", link: "/reference/performance" },
          { text: "AnyTLS + REALITY", link: "/reference/anytls-reality" },
          { text: "設定參考", link: "/reference/configuration" },
          { text: "服務端管理", link: "/aster/overview" },
        ],
        sidebar: [
          {
            text: "開始使用",
            items: [
              { text: "專案介紹", link: "/guide/introduction" },
              { text: "快速開始", link: "/guide/getting-started" },
              { text: "設定概念", link: "/guide/configuration" },
            ],
          },
          {
            text: "實戰教學",
            items: [
              { text: "教學總覽", link: "/tutorials/" },
              { text: "第一個代理設定", link: "/tutorials/first-proxy" },
              { text: "路由與 DNS 分流", link: "/tutorials/routing-dns" },
              { text: "AnyTLS + REALITY", link: "/tutorials/anytls-reality" },
              { text: "使用者與訂閱（服務端）", link: "/tutorials/user-management" },
              { text: "Linux VPS（服務端）", link: "/tutorials/linux-production" },
              { text: "故障排查手冊", link: "/tutorials/troubleshooting" },
            ],
          },
          {
            text: "參考",
            items: [
              { text: "OpenWrt Kernel DIRECT", link: "/deployment/openwrt" },
              { text: "Aster 跟 Mihomo 有什麼不同", link: "/reference/mihomo-differences" },
              { text: "效能優化與基準", link: "/reference/performance" },
              { text: "AnyTLS + REALITY", link: "/reference/anytls-reality" },
              { text: "命令列與環境變數", link: "/reference/cli" },
              { text: "設定總覽", link: "/reference/configuration" },
              { text: "流量治理與報表", link: "/reference/traffic-control" },
              { text: "本機接收連線", link: "/reference/inbounds" },
              { text: "遠端節點與分組", link: "/reference/outbounds" },
              { text: "規則與 DNS", link: "/reference/routing-dns" },
            ],
          },
          {
            text: "Aster 服務端管理",
            items: [
              { text: "功能說明", link: "/aster/overview" },
              { text: "程式控制介面", link: "/aster/api" },
              { text: "安全與資料儲存", link: "/aster/security" },
            ],
          },
          {
            text: "部署",
            items: [
              { text: "Docker", link: "/deployment/docker" },
              { text: "Linux 與 systemd", link: "/deployment/linux" },
              { text: "OpenWrt 與 Nikki", link: "/deployment/openwrt" },
            ],
          },
          {
            text: "開發",
            items: [
              { text: "架構", link: "/development/architecture" },
              { text: "建置與測試", link: "/development/build-test" },
              { text: "疑難排解", link: "/troubleshooting" },
            ],
          },
        ],
        editLink: {
          pattern:
            "https://github.com/Miku0139oao/aster-core/edit/main/docs/:path",
          text: "在 GitHub 編輯此頁",
        },
      },
    },
    en: {
      label: "English",
      lang: "en",
      link: "/en/",
      title: "Aster Core",
      description: enDescription,
      head: [
        ["meta", { property: "og:locale", content: "en" }],
        ["meta", { property: "og:title", content: "Aster Core Docs" }],
        ["meta", { property: "og:description", content: enDescription }],
      ],
      themeConfig: {
        siteTitle: "Aster Core Docs",
        outline: {
          level: [2, 3],
          label: "On this page",
        },
        docFooter: {
          prev: "Previous",
          next: "Next",
        },
        lastUpdated: {
          text: "Last updated",
          formatOptions: {
            dateStyle: "medium",
            timeStyle: "short",
          },
        },
        returnToTopLabel: "Back to top",
        sidebarMenuLabel: "Menu",
        darkModeSwitchLabel: "Appearance",
        lightModeSwitchTitle: "Switch to light theme",
        darkModeSwitchTitle: "Switch to dark theme",
        langMenuLabel: "Change language",
        nav: [
          { text: "Home", link: "/en/" },
          { text: "Downloads", link: "/en/downloads" },
          { text: "Changelog", link: "/en/changelog" },
          {
            text: "Tutorials",
            items: [
              { text: "Tutorial index", link: "/en/tutorials/" },
              { text: "First proxy profile", link: "/en/tutorials/first-proxy" },
              { text: "Routing and DNS", link: "/en/tutorials/routing-dns" },
              { text: "AnyTLS + REALITY", link: "/en/tutorials/anytls-reality" },
              { text: "Users and subscriptions (server)", link: "/en/tutorials/user-management" },
              { text: "Linux VPS (server)", link: "/en/tutorials/linux-production" },
              { text: "Troubleshooting", link: "/en/tutorials/troubleshooting" },
            ],
          },
          { text: "How Aster differs from Mihomo", link: "/en/reference/mihomo-differences" },
          { text: "OpenWrt Kernel DIRECT", link: "/en/deployment/openwrt" },
          { text: "Performance", link: "/en/reference/performance" },
          { text: "AnyTLS + REALITY", link: "/en/reference/anytls-reality" },
          { text: "Configuration", link: "/en/reference/configuration" },
          { text: "Server management", link: "/en/aster/overview" },
        ],
        sidebar: [
          {
            text: "Getting started",
            items: [
              { text: "Introduction", link: "/en/guide/introduction" },
              { text: "Quick start", link: "/en/guide/getting-started" },
              { text: "Configuration concepts", link: "/en/guide/configuration" },
              { text: "Downloads", link: "/en/downloads" },
              { text: "Changelog", link: "/en/changelog" },
            ],
          },
          {
            text: "Tutorials",
            items: [
              { text: "Tutorial index", link: "/en/tutorials/" },
              { text: "First proxy profile", link: "/en/tutorials/first-proxy" },
              { text: "Routing and DNS", link: "/en/tutorials/routing-dns" },
              { text: "AnyTLS + REALITY", link: "/en/tutorials/anytls-reality" },
              { text: "Users and subscriptions (server)", link: "/en/tutorials/user-management" },
              { text: "Linux VPS (server)", link: "/en/tutorials/linux-production" },
              { text: "Troubleshooting handbook", link: "/en/tutorials/troubleshooting" },
            ],
          },
          {
            text: "Reference",
            items: [
              { text: "OpenWrt Kernel DIRECT", link: "/en/deployment/openwrt" },
              { text: "How Aster differs from Mihomo", link: "/en/reference/mihomo-differences" },
              { text: "Performance and benchmarks", link: "/en/reference/performance" },
              { text: "AnyTLS + REALITY", link: "/en/reference/anytls-reality" },
              { text: "CLI and environment variables", link: "/en/reference/cli" },
              { text: "Configuration overview", link: "/en/reference/configuration" },
              { text: "Traffic control and reports", link: "/en/reference/traffic-control" },
              { text: "Local inbounds", link: "/en/reference/inbounds" },
              { text: "Outbounds and groups", link: "/en/reference/outbounds" },
              { text: "Rules and DNS", link: "/en/reference/routing-dns" },
            ],
          },
          {
            text: "Aster server management",
            items: [
              { text: "Overview", link: "/en/aster/overview" },
              { text: "Admin API", link: "/en/aster/api" },
              { text: "Security and state", link: "/en/aster/security" },
            ],
          },
          {
            text: "Deployment",
            items: [
              { text: "Docker", link: "/en/deployment/docker" },
              { text: "Linux and systemd", link: "/en/deployment/linux" },
              { text: "OpenWrt and Nikki", link: "/en/deployment/openwrt" },
            ],
          },
          {
            text: "Development",
            items: [
              { text: "Architecture", link: "/en/development/architecture" },
              { text: "Build and test", link: "/en/development/build-test" },
              { text: "Troubleshooting", link: "/en/troubleshooting" },
            ],
          },
        ],
        editLink: {
          pattern:
            "https://github.com/Miku0139oao/aster-core/edit/main/docs/:path",
          text: "Edit this page on GitHub",
        },
      },
    },
  },
  themeConfig: {
    logo: "/logo.png",
    socialLinks: [
      {
        icon: "github",
        link: "https://github.com/Miku0139oao/aster-core",
      },
    ],
    search: {
      provider: "local",
      options: {
        locales: {
          root: {
            translations: {
              button: {
                buttonText: "搜尋文件",
                buttonAriaLabel: "搜尋文件",
              },
              modal: {
                noResultsText: "找不到相關結果",
                resetButtonTitle: "清除搜尋",
                footer: {
                  selectText: "選取",
                  navigateText: "切換",
                  closeText: "關閉",
                },
              },
            },
          },
          en: {
            translations: {
              button: {
                buttonText: "Search docs",
                buttonAriaLabel: "Search docs",
              },
              modal: {
                noResultsText: "No results for",
                resetButtonTitle: "Reset search",
                footer: {
                  selectText: "to select",
                  navigateText: "to navigate",
                  closeText: "to close",
                },
              },
            },
          },
        },
      },
    },
  },
});
