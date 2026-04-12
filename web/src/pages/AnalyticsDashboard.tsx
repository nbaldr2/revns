import { useEffect, useState } from 'react'
import { LineChart, Line, BarChart, Bar, PieChart, Pie, Cell, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer } from 'recharts'
import { Activity, Server, Users, Calendar } from 'lucide-react'

import type { AnalyticsSummary, DashboardAnalytics } from '../types'

const COLORS = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6']

export default function AnalyticsDashboard() {
  const [summary, setSummary] = useState<AnalyticsSummary | null>(null)
  const [dashboard, setDashboard] = useState<DashboardAnalytics | null>(null)
  const [timeRange, setTimeRange] = useState<number>(7)
  const [loading, setLoading] = useState<boolean>(true)

  useEffect(() => {
    loadAnalytics()
  }, [timeRange])

  const loadAnalytics = async () => {
    try {
      setLoading(true)
      
      const [summaryRes, dashboardRes] = await Promise.all([
        fetch(`/api/v1/analytics/summary?days=${timeRange}`),
        fetch(`/api/v1/analytics/dashboard?days=${timeRange}`),
      ])

      if (summaryRes.ok) {
        const summaryData = await summaryRes.json()
        setSummary(summaryData)
      }

      if (dashboardRes.ok) {
        const dashboardData = await dashboardRes.json()
        setDashboard(dashboardData)
      }
    } catch (error) {
      console.error('Failed to load analytics:', error)
    } finally {
      setLoading(false)
    }
  }

  const formatNumber = (value: number): string => {
    return new Intl.NumberFormat().format(value)
  }

  const formatPercentage = (value: number): string => {
    return `${value.toFixed(1)}%`
  }

  const formatChartDate = (timestamp: number): string => {
    return new Date(timestamp).toLocaleDateString()
  }

  return (
    <div className="analytics-dashboard">
      <header className="dashboard-header">
        <h1>Analytics Dashboard</h1>
        
        <div className="time-range-selector">
          <label htmlFor="timeRange">Time Range:</label>
          <select
            id="timeRange"
            value={timeRange}
            onChange={(e) => setTimeRange(Number(e.target.value))}
            disabled={loading}
          >
            <option value={1}>Last 24 hours</option>
            <option value={7}>Last 7 days</option>
            <option value={30}>Last 30 days</option>
            <option value={90}>Last 90 days</option>
          </select>
        </div>
      </header>

      {loading ? (
        <div className="loading-state">
          <div className="spinner" />
          <p>Loading analytics...</p>
        </div>
      ) : (
        <>

          {/* Charts Section */}
          {dashboard && dashboard.DailyStats.length > 0 && (
            <section className="charts-grid">
              {/* Domain Growth Chart */}
              <div className="chart-card large">
                <h2>
                  <Activity size={20} />
                  Domain Growth Trend
                </h2>
                <ResponsiveContainer width="100%" height={300}>
                  <LineChart data={dashboard.DailyStats}>
                    <CartesianGrid strokeDasharray="3 3" />
                    <XAxis 
                      dataKey="date" 
                      tickFormatter={formatChartDate}
                      tick={{ fontSize: 12 }}
                    />
                    <YAxis tick={{ fontSize: 12 }} />
                    <Tooltip 
                      labelFormatter={(label: any) => formatChartDate(Number(label))}
                      formatter={(value: any) => [formatNumber(Number(value)), "Domains"]}
                    />
                    <Legend />
                    <Line 
                      type="monotone" 
                      dataKey="total_domains" 
                      stroke="#3b82f6" 
                      strokeWidth={2}
                      name="Total Domains"
                    />
                    <Line 
                      type="monotone" 
                      dataKey="total_providers" 
                      stroke="#10b981" 
                      strokeWidth={2}
                      name="Total Providers"
                    />
                  </LineChart>
                </ResponsiveContainer>
              </div>

              {/* Top Nameservers Pie Chart */}
              <div className="chart-card">
                <h2>
                  <Server size={20} />
                  Top Nameservers
                </h2>
                <ResponsiveContainer width="100%" height={250}>
                  <PieChart>
                    <Pie
                      data={dashboard.TopNameservers.slice(0, 5)}
                      dataKey="total_domains"
                      nameKey="ns"
                      cx="50%"
                      cy="50%"
                      outerRadius={80}
                      label={(props: any) => `${props.ns}: ${props.total_domains}`}
                    >
                      {dashboard.TopNameservers.slice(0, 5).map((_, index) => (
                        <Cell key={`cell-${index}`} fill={COLORS[index % COLORS.length]} />
                      ))}
                    </Pie>
                    <Tooltip formatter={(value: any) => [formatNumber(Number(value)), "Domains"]} />
                  </PieChart>
                </ResponsiveContainer>
              </div>

              {/* Top Providers Bar Chart */}
              {summary?.TopProviders && summary.TopProviders.length > 0 && (
                <div className="chart-card">
                  <h2>
                    <Users size={20} />
                    Top Providers
                  </h2>
                  <ResponsiveContainer width="100%" height={250}>
                    <BarChart data={summary.TopProviders.slice(0, 10)}>
                      <CartesianGrid strokeDasharray="3 3" />
                      <XAxis 
                        dataKey="provider" 
                        tick={{ fontSize: 10 }}
                        angle={-45}
                        textAnchor="end"
                        interval={0}
                      />
                      <YAxis tick={{ fontSize: 12 }} />
                      <Tooltip formatter={(value: any) => [formatNumber(Number(value)), "Domains"]} />
                      <Bar dataKey="domain_count" fill="#8b5cf6" />
                    </BarChart>
                  </ResponsiveContainer>
                </div>
              )}
            </section>
          )}

          {/* Time Range Info */}
          {summary && (
            <section className="time-info">
              <p className="time-range">
                <Calendar size={16} />
                Data range: {new Date(summary.TimeRange.StartDate).toLocaleDateString()} - {new Date(summary.TimeRange.EndDate).toLocaleDateString()}
              </p>
              <p className="data-points">
                {summary.TimeRange.Days} days of data ({summary.CurrentStats.DataPoints} data points)
              </p>
            </section>
          )}
        </>
      )}
    </div>
  )
}
