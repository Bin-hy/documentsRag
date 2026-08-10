<script setup lang="ts">
// 登录页：API Key 输入登录 + 三方登录 Provider 按钮（OIDC / GitHub）
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Key, View, Hide, Link } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'
import { listProviders, providerLoginURL, type ProviderView } from '../api/auth'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()

const apiKey = ref('')
const showKey = ref(false)
const loading = ref(false)
const providers = ref<ProviderView[]>([])
const oidcLoading = ref(false)

// 加载可用的登录 Provider（公开接口，无凭据）
onMounted(async () => {
  try {
    providers.value = await listProviders()
  } catch {
    providers.value = []
  }
  // OIDC 回调：一次性 ticket 换 JWT（?ticket= 由 history.replaceState 立即清除，不残留 URL）
  const ticket = route.query.ticket as string | undefined
  if (ticket) {
    oidcLoading.value = true
    try {
      await auth.loginWithOIDC(ticket)
      ElMessage.success('登录成功')
      const redirect = (route.query.redirect as string) || '/chat'
      router.replace({ path: redirect })
    } catch (err) {
      ElMessage.error((err as Error).message || '登录失败，请重试')
    } finally {
      oidcLoading.value = false
    }
  }
  // 回调失败：?error= 提示
  const errMsg = route.query.error as string | undefined
  if (errMsg) {
    ElMessage.error(errMsg)
  }
})

async function handleLogin() {
  if (!apiKey.value.trim()) {
    ElMessage.warning('请输入 API Key')
    return
  }
  loading.value = true
  try {
    await auth.loginWithAPIKey(apiKey.value.trim())
    ElMessage.success('登录成功')
    const redirect = (route.query.redirect as string) || '/chat'
    router.push(redirect)
  } catch (err) {
    ElMessage.error((err as Error).message || '登录失败，请检查 API Key')
  } finally {
    loading.value = false
  }
}

// 跳转三方登录授权页
function goProvider(p: ProviderView) {
  window.location.href = providerLoginURL(p)
}
</script>

<template>
  <div class="login-page">
    <div class="login-orb orb-a" />
    <div class="login-orb orb-b" />

    <div class="login-shell">
      <div class="login-card">
        <div class="login-logo">
          <span class="login-logo-icon">B</span>
          <h1 class="login-title">BinRag</h1>
          <p class="login-subtitle">企业级文档知识库问答系统</p>
        </div>

        <el-form @submit.prevent="handleLogin">
          <el-form-item>
            <el-input
              v-model="apiKey"
              :type="showKey ? 'text' : 'password'"
              placeholder="请输入 API Key（binrag_ 开头）"
              size="large"
              :prefix-icon="Key"
              @keyup.enter="handleLogin"
            >
              <template #suffix>
                <el-icon class="cursor-pointer" @click="showKey = !showKey">
                  <View v-if="!showKey" />
                  <Hide v-else />
                </el-icon>
              </template>
            </el-input>
          </el-form-item>
          <el-form-item>
            <el-button
              type="primary"
              size="large"
              class="login-btn"
              :loading="loading"
              @click="handleLogin"
            >
              登录
            </el-button>
          </el-form-item>
        </el-form>

        <!-- 三方登录 Provider（动态渲染） -->
        <template v-if="providers.length > 0">
          <el-divider>
            <span class="br-muted divider-text">或使用三方账号</span>
          </el-divider>
          <div class="provider-list">
            <el-button
              v-for="p in providers"
              :key="p.name"
              size="large"
              class="provider-btn"
              :loading="oidcLoading"
              @click="goProvider(p)"
            >
              <el-icon style="margin-right: 6px"><Link /></el-icon>
              通过 {{ p.display_name }} 登录
            </el-button>
          </div>
        </template>

        <p class="login-tip br-muted">
          未配置 API Key？请在服务端配置 bootstrap_api_key 或使用已有的 API Key
        </p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  position: relative;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}

/* 环境光球：柔和的品牌色光晕，营造氛围 */
.login-orb {
  position: absolute;
  border-radius: 50%;
  filter: blur(90px);
  opacity: 0.5;
  pointer-events: none;
}

.orb-a {
  width: 420px;
  height: 420px;
  top: -120px;
  right: 8%;
  background: radial-gradient(circle, rgba(79, 91, 213, 0.5), transparent 65%);
}

.orb-b {
  width: 380px;
  height: 380px;
  bottom: -140px;
  left: 6%;
  background: radial-gradient(circle, rgba(125, 133, 238, 0.42), transparent 65%);
}

html.dark .orb-a {
  opacity: 0.35;
}
html.dark .orb-b {
  opacity: 0.3;
}

/* 玻璃卡片（双贝泽尔） */
.login-shell {
  position: relative;
  z-index: 1;
  width: 400px;
  padding: 6px;
  border-radius: 26px;
  background: color-mix(in srgb, var(--br-bg-inset) 78%, transparent);
  border: 1px solid var(--br-border);
  box-shadow: var(--br-shadow-lg);
  backdrop-filter: blur(18px);
  -webkit-backdrop-filter: blur(18px);
  animation: card-in 480ms var(--br-ease) both;
}

@keyframes card-in {
  from {
    opacity: 0;
    transform: translateY(16px) scale(0.98);
  }
  to {
    opacity: 1;
    transform: none;
  }
}

.login-card {
  padding: 34px 30px 24px;
  border-radius: 20px;
  background: color-mix(in srgb, var(--br-bg-card) 92%, transparent);
  box-shadow: inset 0 1px 1px rgba(255, 255, 255, 0.45);
}

html.dark .login-card {
  box-shadow: inset 0 1px 1px rgba(255, 255, 255, 0.06);
}

.login-logo {
  text-align: center;
  margin-bottom: 26px;
}

.login-logo-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 58px;
  height: 58px;
  border-radius: 17px;
  background: linear-gradient(135deg, #5f6be0, #404ab0);
  color: #fff;
  font-size: 30px;
  font-weight: 700;
  box-shadow: 0 14px 30px rgba(79, 91, 213, 0.32);
}

.login-title {
  margin: 14px 0 4px;
  font-size: 25px;
  font-weight: 700;
  letter-spacing: -0.02em;
}

.login-subtitle {
  margin: 0;
  color: var(--br-text-secondary);
  font-size: 13px;
}

.login-btn {
  width: 100%;
  border-radius: var(--br-radius-pill);
  font-weight: 600;
  letter-spacing: 0.02em;
}

.divider-text {
  font-size: 12px;
}

.provider-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.provider-btn {
  width: 100%;
  border-radius: var(--br-radius-md);
}

.login-tip {
  margin: 14px 0 0;
  font-size: 12px;
  text-align: center;
}

.cursor-pointer {
  cursor: pointer;
}
</style>
