#ifndef _NIMA_KEYFRAMETRIGGER_HPP_
#define _NIMA_KEYFRAMETRIGGER_HPP_

#include "KeyFrame.hpp"

namespace nima
{
	class ActorComponent;

	class KeyFrameTrigger : public KeyFrame
	{
		typedef KeyFrame Base;

		public:	
			KeyFrameTrigger();
			virtual ~KeyFrameTrigger();

			bool read(BlockReader* reader, ActorComponent* component) override;
			void setNext(KeyFrame* frame) override;
			void apply(ActorComponent* component, float mix) override;
			void applyInterpolation(ActorComponent* component, float time, KeyFrame* toFrame, float mix) override;
	};
}

#endif