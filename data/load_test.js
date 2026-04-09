import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('errors');

// Test configuration
export const options = {
  stages: [
    { duration: '2m', target: 100 },    // Ramp up to 100 users
    { duration: '5m', target: 100 },    // Stay at 100 users
    { duration: '2m', target: 200 },    // Ramp up to 200 users
    { duration: '5m', target: 200 },    // Stay at 200 users
    { duration: '2m', target: 0 },      // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'],    // 95% of requests under 500ms
    http_req_failed: ['rate<0.01'],      // Less than 1% errors
    errors: ['rate<0.01'],
  },
};

// Sample nameservers to test
const nameservers = [
  'ns1.cloudflare.com',
  'ns2.cloudflare.com',
  'ns1.google.com',
  'ns2.google.com',
  'ns1.awsdns.com',
  'ns2.awsdns.com',
  'ns1.godaddy.com',
  'ns2.godaddy.com',
  'ns1.bluehost.com',
  'ns2.bluehost.com',
];

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
  // Pick a random nameserver
  const ns = nameservers[Math.floor(Math.random() * nameservers.length)];
  
  // Test the main endpoint
  const response = http.get(`${BASE_URL}/api/v1/ns/${ns}?page=1&limit=100`, {
    tags: { name: 'GetReverseNS' },
  });
  
  // Check response
  const success = check(response, {
    'status is 200': (r) => r.status === 200,
    'response time < 500ms': (r) => r.timings.duration < 500,
    'has domains array': (r) => {
      try {
        const body = JSON.parse(r.body);
        return Array.isArray(body.domains);
      } catch {
        return false;
      }
    },
  });
  
  errorRate.add(!success);
  
  // Random sleep between 0.1 and 1 second
  sleep(Math.random() * 0.9 + 0.1);
}

// Health check test
export function healthCheck() {
  const response = http.get(`${BASE_URL}/health/ready`);
  
  check(response, {
    'health check status is 200': (r) => r.status === 200,
    'health check returns ok': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.status === 'ok';
      } catch {
        return false;
      }
    },
  });
  
  sleep(1);
}

// Stress test with high pagination
export function paginationTest() {
  const ns = nameservers[0];
  
  // Test different page sizes
  const limits = [10, 50, 100, 500];
  const limit = limits[Math.floor(Math.random() * limits.length)];
  
  const response = http.get(`${BASE_URL}/api/v1/ns/${ns}?page=1&limit=${limit}`, {
    tags: { name: 'PaginationTest' },
  });
  
  check(response, {
    'pagination status is 200': (r) => r.status === 200,
    'correct limit returned': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.limit === limit;
      } catch {
        return false;
      }
    },
  });
  
  sleep(0.5);
}
