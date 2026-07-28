include $(TOPDIR)/rules.mk

PKG_NAME:=aster-core
PKG_VERSION:=1.14.514
PKG_RELEASE:=2

PKG_SOURCE:=$(PKG_NAME)-$(PKG_VERSION).tar.gz
PKG_SOURCE_SUBDIR:=$(PKG_NAME)-$(PKG_VERSION)
PKG_SOURCE_PROTO:=git
PKG_SOURCE_URL:=https://github.com/Miku0139oao/aster-core.git
PKG_SOURCE_VERSION:=main
PKG_MIRROR_HASH:=skip

PKG_LICENSE:=GPL-3.0-only
PKG_LICENSE_FILES:=LICENSE
PKG_MAINTAINER:=Miku0139oao

PKG_BUILD_DEPENDS:=golang/host upx/host
PKG_BUILD_PARALLEL:=1
PKG_BUILD_FLAGS:=no-mips16

PKG_BUILD_VERSION:=v$(PKG_VERSION)R$(PKG_RELEASE)
PKG_BUILD_DIR:=$(BUILD_DIR)/$(PKG_NAME)-$(PKG_VERSION)
PKG_BUILD_TIME:=$(shell date -u -Iseconds)

ifneq ($(ASTER_CORE_LOCAL_SOURCE),)
  USE_SOURCE_DIR:=$(ASTER_CORE_LOCAL_SOURCE)
endif

GO_PKG:=github.com/Miku0139oao/aster-core
GO_PKG_BUILD_PKG:=$(GO_PKG)
GO_PKG_INSTALL_BIN_PATH:=/usr/libexec
GO_PKG_TAGS:=with_gvisor
GO_PKG_LDFLAGS_X:=$(GO_PKG)/constant.Version=$(PKG_BUILD_VERSION) $(GO_PKG)/constant.BuildTime=$(PKG_BUILD_TIME)

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

# The packed binary must not be stripped again during IPK assembly.
RSTRIP:=:

define Package/aster-core
  SECTION:=net
  CATEGORY:=Network
  TITLE:=Mihomo-compatible universal proxy core
  URL:=https://github.com/Miku0139oao/aster-core
  DEPENDS:=$(GO_ARCH_DEPENDS) +ca-bundle +ip-full +kmod-inet-diag +kmod-tun
  PROVIDES:=mihomo
  CONFLICTS:=mihomo-alpha mihomo-meta
  ALTERNATIVES:=400:/usr/bin/mihomo:/usr/libexec/aster-core
endef

define Package/aster-core/description
 Aster Core exposes Mihomo-compatible YAML and controller APIs. It provides
 /usr/bin/mihomo through OpenWrt alternatives for use with Nikki.
endef

define Build/Compile
	$(call GoPackage/Build/Compile)
	$(TARGET_CROSS)strip --strip-all $(GO_PKG_BUILD_BIN_DIR)/$(PKG_NAME)
	$(STAGING_DIR_HOSTPKG)/bin/upx --best --lzma $(GO_PKG_BUILD_BIN_DIR)/$(PKG_NAME)
endef

define Package/aster-core/install
	$(call GoPackage/Package/Install/Bin,$(1))
endef

$(eval $(call GoBinPackage,aster-core))
$(eval $(call BuildPackage,aster-core))
