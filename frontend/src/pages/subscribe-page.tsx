import { useState } from 'react'
import { AtSign, Bell, CircleCheck, Lock, Mail } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { subscribe } from '@/lib/api'
import type { BlogConfig } from '@/types'

type SubscribePageProps = {
  config: BlogConfig
}

export function SubscribePage({ config }: SubscribePageProps) {
  const [email, setEmail] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [success, setSuccess] = useState(false)

  const onSubmit = async () => {
    const value = email.trim()
    if (!value) {
      return
    }
    setSubmitting(true)
    try {
      await subscribe(value)
      setEmail('')
      setSuccess(true)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="flex min-h-full items-center justify-center bg-white px-4 py-8 sm:px-8 lg:px-[120px] lg:py-20">
      <Card className="w-full max-w-[560px] gap-0 rounded-2xl border-[#E5E7EB] py-0 shadow-none">
        <CardContent className="p-12">
        <div className="mb-8 flex justify-center">
          <div className="flex h-20 w-20 items-center justify-center rounded-full bg-[#EEF2FF]">
            <Mail className="h-9 w-9 text-[#3141F5]" />
          </div>
        </div>

        <h1 className="mb-3 text-center text-[28px] font-bold text-[#181A1B]">{config.subscribe.title}</h1>
        <p className="mb-8 text-center text-[15px] leading-[1.7] text-[#5E6573]">{config.subscribe.description}</p>

        <div className="mb-4 space-y-4">
          <div className="relative">
            <AtSign className="absolute left-4 top-4 h-5 w-5 text-[#A1A1AA]" />
            <Input
              type="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              placeholder={config.subscribe.placeholder}
              className="h-[52px] rounded-lg border-[#E5E7EB] pl-12 text-[15px]"
            />
          </div>
          <Button
            onClick={onSubmit}
            disabled={submitting}
            className="h-[52px] w-full rounded-lg bg-[#3141F5] text-base font-semibold hover:bg-[#192bf4]"
          >
            <Bell className="mr-2 h-4 w-4" />
            {config.subscribe.button_text}
          </Button>
        </div>

        <div className="mb-6 space-y-3">
          {config.subscribe.benefits.map((benefit) => (
            <div key={benefit} className="flex items-center gap-2.5 text-sm text-[#5E6573]">
              <CircleCheck className="h-[18px] w-[18px] text-[#10B981]" />
              {benefit}
            </div>
          ))}
        </div>

        <div className="flex items-center justify-center gap-1.5 text-xs text-[#A1A1AA]">
          <Lock className="h-3.5 w-3.5" />
          <span>{success ? '订阅成功，感谢关注！' : config.subscribe.privacy_note}</span>
        </div>
        </CardContent>
      </Card>
    </div>
  )
}
