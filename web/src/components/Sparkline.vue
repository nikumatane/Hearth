<script setup lang="ts">
import { computed } from "vue";
import type { MetricPoint } from "../api";

const props = withDefaults(
  defineProps<{
    points: MetricPoint[];
    color?: string;
    height?: number;
    fill?: boolean;
  }>(),
  {
    color: "#7c9cff",
    height: 72,
    fill: true
  }
);

const width = 320;
const padding = 4;

const geometry = computed(() => {
  if (props.points.length < 2) {
    return { line: "", area: "" };
  }
  const values = props.points.map((point) => point.value);
  const min = Math.min(...values);
  const max = Math.max(...values);
  const range = Math.max(max - min, 1);
  const coordinates = values.map((value, index) => {
    const x = padding + (index / (values.length - 1)) * (width - padding * 2);
    const y =
      padding +
      (1 - (value - min) / range) * (props.height - padding * 2);
    return [x, y];
  });
  const line = coordinates
    .map(([x, y], index) => `${index === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`)
    .join(" ");
  const area = `${line} L${width - padding},${props.height} L${padding},${props.height} Z`;
  return { line, area };
});
</script>

<template>
  <svg
    class="sparkline"
    :viewBox="`0 0 ${width} ${height}`"
    preserveAspectRatio="none"
    role="img"
    aria-label="资源使用趋势"
  >
    <defs>
      <linearGradient :id="`fill-${color.replace('#', '')}`" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0%" :stop-color="color" stop-opacity=".28" />
        <stop offset="100%" :stop-color="color" stop-opacity="0" />
      </linearGradient>
    </defs>
    <path
      v-if="fill"
      :d="geometry.area"
      :fill="`url(#fill-${color.replace('#', '')})`"
    />
    <path
      :d="geometry.line"
      fill="none"
      :stroke="color"
      stroke-width="2.5"
      vector-effect="non-scaling-stroke"
      stroke-linecap="round"
      stroke-linejoin="round"
    />
  </svg>
</template>
