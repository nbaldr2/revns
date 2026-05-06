import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import type { HostingProvider } from '../types'
import { formatNumber } from '../utils/format'

interface TopProvidersChartProps {
  data: HostingProvider[]
}

const CustomTooltip = ({ active, payload, label }: any) => {
  if (active && payload && payload.length) {
    return (
      <div className="custom-tooltip" style={{
        background: 'rgba(15, 23, 42, 0.9)',
        border: '1px solid rgba(59, 130, 246, 0.3)',
        padding: '10px',
        borderRadius: '8px',
        color: '#fff'
      }}>
        <p className="label" style={{ margin: 0, fontWeight: 600 }}>{label}</p>
        <p className="intro" style={{ margin: 0, color: '#60a5fa' }}>
          {formatNumber(payload[0].value)} domains
        </p>
      </div>
    )
  }
  return null
}

export default function TopProvidersChart({ data }: TopProvidersChartProps) {
  // Sort data ascending for vertical chart to have top at the top
  const chartData = [...data].reverse().map(d => ({
    name: d.provider,
    domains: d.domain_count
  }))

  return (
    <div style={{ width: '100%', height: '300px', marginTop: '1rem' }}>
      <ResponsiveContainer width="100%" height="100%">
        <BarChart
          data={chartData}
          layout="vertical"
          margin={{ top: 5, right: 30, left: 20, bottom: 5 }}
        >
          <CartesianGrid strokeDasharray="3 3" stroke="rgba(147, 197, 253, 0.1)" horizontal={false} />
          <XAxis type="number" hide />
          <YAxis 
            dataKey="name" 
            type="category" 
            axisLine={false} 
            tickLine={false} 
            tick={{ fill: '#93c5fd', fontSize: 12 }} 
            width={120}
          />
          <Tooltip content={<CustomTooltip />} cursor={{ fill: 'rgba(59, 130, 246, 0.1)' }} />
          <Bar 
            dataKey="domains" 
            fill="url(#colorUv)" 
            radius={[0, 4, 4, 0]} 
            barSize={20}
          />
          <defs>
            <linearGradient id="colorUv" x1="0" y1="0" x2="1" y2="0">
              <stop offset="0%" stopColor="#2563eb" stopOpacity={0.8} />
              <stop offset="100%" stopColor="#60a5fa" stopOpacity={1} />
            </linearGradient>
          </defs>
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}
