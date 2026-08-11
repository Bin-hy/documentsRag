import DefaultTheme from 'vitepress/theme'
import type { Theme } from 'vitepress'
import './custom.css'

import FeatureCard from './components/FeatureCard.vue'
import ArchitectureDiagram from './components/ArchitectureDiagram.vue'
import HighlightBlock from './components/HighlightBlock.vue'
import Steps from './components/Steps.vue'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component('FeatureCard', FeatureCard)
    app.component('ArchitectureDiagram', ArchitectureDiagram)
    app.component('HighlightBlock', HighlightBlock)
    app.component('Steps', Steps)
  },
} satisfies Theme
