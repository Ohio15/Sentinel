import { Header } from '@/components/layout';
import { Card, CardContent } from '@/components/ui';
import { Bell } from 'lucide-react';

export function AlertRules() {
  return (
    <div>
      <Header title="Alert Rules" subtitle="Configure alerting rules" />
      <div className="p-6">
        <Card>
          <CardContent>
            <div className="text-center py-12">
              <Bell className="w-12 h-12 text-text-secondary mx-auto mb-4" />
              <h3 className="text-lg font-medium text-text-primary mb-2">Alert Rules</h3>
              <p className="text-text-secondary">Alert rules management coming soon</p>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
