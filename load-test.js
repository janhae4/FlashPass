import http from 'k6/http';
import { check } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';

// Custom metric counters
const acceptedCount = new Counter('tickets_accepted_202');
const soldOutCount = new Counter('tickets_sold_out_409');
const errorCount = new Counter('tickets_error_5xx');
const bookingDuration = new Trend('booking_duration_ms');

const BASE_URL = __ENV.API_URL || 'http://localhost:8081';
const EVENT_ID = __ENV.EVENT_ID || 'e9b8849b-7e61-4648-936c-941a329fa9c1';
const TOTAL_REQUESTS = parseInt(__ENV.TOTAL_REQUESTS || '20000', 10);
const CONCURRENT_VUS = parseInt(__ENV.VUS || '100', 10);

export const options = {
  scenarios: {
    flash_sale_surge: {
      executor: 'shared-iterations',
      vus: CONCURRENT_VUS,
      iterations: TOTAL_REQUESTS,
      maxDuration: '120s',
    },
  },
  thresholds: {
    tickets_accepted_202: ['count==1000'], // Exactly 1,000 tickets reserved
    tickets_sold_out_409: [`count==${TOTAL_REQUESTS - 1000}`], // Exactly remaining requests fail-fast
    tickets_error_5xx: ['count==0'],       // Zero 5xx server errors
    http_req_duration: ['p(95)<30', 'p(99)<60'], // Low latency invariant
  },
};

export default function () {
  const userId = `vu-${__VU}-user-${__ITER}`;
  const idempotencyKey = `idemp-${__VU}-${__ITER}-${Date.now()}`;

  const payload = JSON.stringify({
    event_id: EVENT_ID,
    user_id: userId,
    idempotency_key: idempotencyKey,
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
    },
    timeout: '10s',
  };

  const startTime = Date.now();
  const res = http.post(`${BASE_URL}/api/v1/tickets/book`, payload, params);
  bookingDuration.add(Date.now() - startTime);

  if (res.status === 202) {
    acceptedCount.add(1);
  } else if (res.status === 409) {
    soldOutCount.add(1);
  } else {
    errorCount.add(1);
    console.error(`Unexpected response: status=${res.status}, body=${res.body}`);
  }

  check(res, {
    'status is 202 or 409': (r) => r.status === 202 || r.status === 409,
  });
}
