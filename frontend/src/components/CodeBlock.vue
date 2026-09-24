<script setup>
import {computed} from 'vue';
import hljs from 'highlight.js/lib/core';
import bash from 'highlight.js/lib/languages/bash';
import json from 'highlight.js/lib/languages/json';
import javascript from 'highlight.js/lib/languages/javascript';
import {toast} from '../runtime.js';
hljs.registerLanguage('bash',bash);hljs.registerLanguage('json',json);hljs.registerLanguage('javascript',javascript);
const props=defineProps({code:String,language:{default:'bash'},label:String});
const highlighted=computed(()=>hljs.highlight(props.code||'',{language:props.language}).value);
async function copy(){try{await navigator.clipboard.writeText(props.code);toast('示例已复制');}catch{toast('复制失败');}}
</script>
<template><div class="code-block"><div class="code-toolbar"><span>{{ label || language.toUpperCase() }}</span><UiButton icon="clipboard" aria-label="复制代码示例" @click="copy">复制</UiButton></div><pre><code v-html="highlighted"></code></pre></div></template>
