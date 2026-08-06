<script setup lang="ts">
// 登录页：输入 API Key 作为登录凭据
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Key, View, Hide } from '@element-plus/icons-vue'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()

const apiKey = ref('')
const showKey = ref(false)
const loading = ref(false)

async function handleLogin() {
  if (!apiKey.value.trim()) {
    ElMessage.warning('请输入 API Key')
    return
  }
  loading.value = true
  try {
    await auth.login(apiKey.value.trim())
    ElMessage.success('登录成功')
    const redirect = (route.query.redirect as string) || '/chat'
    router.push(redirect)
  } catch (err) {
    ElMessage.error((err as Error).message || '登录失败，请检查 API Key')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-page">
    <el-card class="login-card" shadow="always">
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

      <p class="login-tip br-muted">
        未配置 API Key？请在服务端配置 bootstrap_api_key 或使用已有的 API Key
      </p>
    </el-card>
  </div>
</template>

<style scoped>
.login-page {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--br-bg);
}

.login-card {
  width: 380px;
  padding: 12px 8px;
}

.login-logo {
  text-align: center;
  margin-bottom: 24px;
}

.login-logo-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 56px;
  height: 56px;
  border-radius: 14px;
  background: var(--br-primary);
  color: #fff;
  font-size: 28px;
  font-weight: 700;
}

.login-title {
  margin: 12px 0 4px;
  font-size: 24px;
}

.login-subtitle {
  margin: 0;
  color: var(--br-text-secondary);
  font-size: 13px;
}

.login-btn {
  width: 100%;
}

.login-tip {
  margin: 8px 0 0;
  font-size: 12px;
  text-align: center;
}

.cursor-pointer {
  cursor: pointer;
}
</style>
